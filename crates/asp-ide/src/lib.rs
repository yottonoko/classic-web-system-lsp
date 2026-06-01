use std::{collections::HashMap, sync::OnceLock};

use asp_sidecar_protocol::{SourceMapSegment, VirtualDocument};
use ropey::Rope;
use salsa::Setter;
use serde_json::Value;

const VBSCRIPT_BUILTIN_CATALOG_JSON: &str =
    include_str!("../../../packages/core/src/vbscript-builtin-catalog.json");

#[salsa::input]
struct SourceFile {
    #[returns(ref)]
    uri: String,
    #[returns(ref)]
    text: String,
}

#[salsa::input]
struct WorkspaceSettings {
    #[returns(ref)]
    json: String,
}

#[salsa::tracked(returns(clone))]
fn parse_asp(
    db: &dyn salsa::Database,
    source_file: SourceFile,
    settings: WorkspaceSettings,
) -> Result<Value, String> {
    let settings_value =
        serde_json::from_str(settings.json(db)).map_err(|error| error.to_string())?;
    asp_analysis::parse_asp_skeleton_once(
        source_file.uri(db),
        source_file.text(db),
        &settings_value,
    )
}

#[salsa::tracked(returns(clone))]
fn parser_diagnostics(
    db: &dyn salsa::Database,
    source_file: SourceFile,
    settings: WorkspaceSettings,
) -> Result<Vec<Value>, String> {
    let settings_value =
        serde_json::from_str(settings.json(db)).map_err(|error| error.to_string())?;
    asp_analysis::parser_diagnostics_once(
        source_file.uri(db),
        source_file.text(db),
        &settings_value,
    )
}

#[salsa::tracked(returns(clone))]
fn vb_symbols(
    db: &dyn salsa::Database,
    source_file: SourceFile,
    settings: WorkspaceSettings,
) -> Result<Vec<Value>, String> {
    let settings_value =
        serde_json::from_str(settings.json(db)).map_err(|error| error.to_string())?;
    asp_analysis::vb_symbols_once(source_file.uri(db), source_file.text(db), &settings_value)
}

#[salsa::tracked(returns(clone))]
fn vb_diagnostics(
    db: &dyn salsa::Database,
    source_file: SourceFile,
    settings: WorkspaceSettings,
) -> Result<Vec<Value>, String> {
    let settings_value =
        serde_json::from_str(settings.json(db)).map_err(|error| error.to_string())?;
    asp_analysis::vb_diagnostics_once(source_file.uri(db), source_file.text(db), &settings_value)
}

#[salsa::tracked(returns(clone))]
fn include_refs(
    db: &dyn salsa::Database,
    source_file: SourceFile,
    settings: WorkspaceSettings,
) -> Result<Vec<Value>, String> {
    let settings_value =
        serde_json::from_str(settings.json(db)).map_err(|error| error.to_string())?;
    asp_analysis::include_refs_once(source_file.uri(db), source_file.text(db), &settings_value)
}

#[salsa::tracked(returns(clone))]
fn document_diagnostics(
    db: &dyn salsa::Database,
    source_file: SourceFile,
    settings: WorkspaceSettings,
) -> Result<Vec<Value>, String> {
    let settings_value =
        serde_json::from_str(settings.json(db)).map_err(|error| error.to_string())?;
    asp_analysis::analyze_document_once(source_file.uri(db), source_file.text(db), &settings_value)
}

#[salsa::db]
#[derive(Clone, Default)]
struct IdeDatabase {
    storage: salsa::Storage<Self>,
}

#[salsa::db]
impl salsa::Database for IdeDatabase {}

pub struct Ide {
    db: IdeDatabase,
    documents: HashMap<String, OpenDocument>,
    indexed_documents: HashMap<String, OpenDocument>,
    settings: WorkspaceSettingsState,
}

#[derive(Clone, Debug)]
pub struct MappedVirtualDocument {
    pub document: VirtualDocument,
    pub source_map: Vec<SourceMapSegment>,
    source_text: String,
}

impl MappedVirtualDocument {
    pub fn remap_diagnostic(&self, diagnostic: Value) -> Option<Value> {
        let range = self.remap_range(diagnostic.get("range")?)?;
        let mut diagnostic = diagnostic.as_object()?.clone();
        diagnostic.insert("range".to_string(), range);
        Some(Value::Object(diagnostic))
    }

    pub fn virtual_position_for_source_position(&self, position: TextPosition) -> Option<Value> {
        let source_offset = position_to_utf16_offset(
            &self.source_text,
            position.line.try_into().ok()?,
            position.character.try_into().ok()?,
        )?;
        let segment = self.source_map.iter().find(|segment| {
            source_offset >= segment.source_start && source_offset <= segment.source_end
        })?;
        let virtual_offset = segment.virtual_start + (source_offset - segment.source_start);
        let (line, character) = utf16_position_at(&self.document.text, virtual_offset)?;
        Some(serde_json::json!({ "line": line, "character": character }))
    }

    pub fn virtual_range_for_source_range(&self, range: TextRange) -> Option<Value> {
        let source_start = position_to_utf16_offset(
            &self.source_text,
            range.start.line.try_into().ok()?,
            range.start.character.try_into().ok()?,
        )?;
        let source_end = position_to_utf16_offset(
            &self.source_text,
            range.end.line.try_into().ok()?,
            range.end.character.try_into().ok()?,
        )?;
        let segment = self.source_map.iter().find(|segment| {
            let last_offset = source_start.max(source_end.saturating_sub(1));
            source_start >= segment.source_start && last_offset < segment.source_end
        })?;
        let virtual_start = segment.virtual_start + (source_start - segment.source_start);
        let virtual_end = segment.virtual_start + (source_end - segment.source_start);
        let (start_line, start_character) = utf16_position_at(&self.document.text, virtual_start)?;
        let (end_line, end_character) = utf16_position_at(&self.document.text, virtual_end)?;
        Some(serde_json::json!({
            "start": { "line": start_line, "character": start_character },
            "end": { "line": end_line, "character": end_character },
        }))
    }

    pub fn remap_lsp_value(&self, value: Value) -> Value {
        self.remap_value(value)
    }

    fn remap_value(&self, value: Value) -> Value {
        match value {
            Value::Array(items) => Value::Array(
                items
                    .into_iter()
                    .map(|item| self.remap_value(item))
                    .collect(),
            ),
            Value::Object(mut object) => {
                if is_lsp_range_object(&object) {
                    return self
                        .remap_range(&Value::Object(object))
                        .unwrap_or(Value::Null);
                }
                for (key, value) in object.iter_mut() {
                    if matches!(
                        key.as_str(),
                        "range" | "selectionRange" | "targetSelectionRange"
                    ) {
                        continue;
                    }
                    let remapped = self.remap_value(std::mem::take(value));
                    *value = remapped;
                }
                if let Some(range) = object
                    .get("range")
                    .and_then(|range| self.remap_range(range))
                {
                    object.insert("range".to_string(), range);
                }
                if let Some(range) = object
                    .get("selectionRange")
                    .and_then(|range| self.remap_range(range))
                {
                    object.insert("selectionRange".to_string(), range);
                }
                if let Some(range) = object
                    .get("targetSelectionRange")
                    .and_then(|range| self.remap_range(range))
                {
                    object.insert("targetSelectionRange".to_string(), range);
                }
                if let Some(range) = self.remap_folding_range(&object) {
                    object.insert(
                        "startLine".to_string(),
                        range.pointer("/start/line").cloned().unwrap_or(Value::Null),
                    );
                    object.insert(
                        "endLine".to_string(),
                        range.pointer("/end/line").cloned().unwrap_or(Value::Null),
                    );
                }
                if object.get("uri").and_then(Value::as_str) == Some(self.document.uri.as_str()) {
                    object.insert("uri".to_string(), Value::String(self.source_uri()));
                }
                Value::Object(object)
            }
            value => value,
        }
    }

    fn remap_range(&self, range: &Value) -> Option<Value> {
        let start_offset = self.virtual_offset(range.get("start")?)?;
        let end_offset = self.virtual_offset(range.get("end")?)?;
        self.remap_virtual_offsets(start_offset, end_offset)
    }

    fn remap_folding_range(&self, object: &serde_json::Map<String, Value>) -> Option<Value> {
        let start_line = object.get("startLine")?.as_u64()?;
        let end_line = object.get("endLine")?.as_u64()?;
        let start_character = object
            .get("startCharacter")
            .and_then(Value::as_u64)
            .unwrap_or(0);
        let end_character = object
            .get("endCharacter")
            .and_then(Value::as_u64)
            .unwrap_or(0);
        let start_offset = position_to_utf16_offset(
            &self.document.text,
            start_line.try_into().ok()?,
            start_character.try_into().ok()?,
        )?;
        let end_offset = position_to_utf16_offset(
            &self.document.text,
            end_line.try_into().ok()?,
            end_character.try_into().ok()?,
        )?;
        self.remap_virtual_offsets(start_offset, end_offset)
    }

    fn remap_virtual_offsets(&self, start_offset: usize, end_offset: usize) -> Option<Value> {
        let segment = self.source_map.iter().find(|segment| {
            let last_offset = start_offset.max(end_offset.saturating_sub(1));
            start_offset >= segment.virtual_start && last_offset < segment.virtual_end
        })?;
        let source_start = segment.source_start + (start_offset - segment.virtual_start);
        let source_end = segment.source_start + (end_offset - segment.virtual_start);
        let (start_line, start_character) = utf16_position_at(&self.source_text, source_start)?;
        let (end_line, end_character) = utf16_position_at(&self.source_text, source_end)?;
        Some(serde_json::json!({
            "start": { "line": start_line, "character": start_character },
            "end": { "line": end_line, "character": end_character },
        }))
    }

    fn virtual_offset(&self, position: &Value) -> Option<usize> {
        position_to_utf16_offset(
            &self.document.text,
            position.get("line")?.as_u64()?.try_into().ok()?,
            position.get("character")?.as_u64()?.try_into().ok()?,
        )
    }

    fn source_uri(&self) -> String {
        self.document
            .uri
            .strip_suffix(&format!(".{}.virtual", self.document.language_id))
            .unwrap_or(&self.document.uri)
            .to_string()
    }
}

impl Default for Ide {
    fn default() -> Self {
        let db = IdeDatabase::default();
        let settings = WorkspaceSettingsState::new(&db);
        Self {
            db,
            documents: HashMap::new(),
            indexed_documents: HashMap::new(),
            settings,
        }
    }
}

impl Ide {
    pub fn set_open_document(&mut self, uri: String, text: String) {
        let document = OpenDocument::new(&self.db, uri.clone(), text);
        self.documents.insert(uri, document);
    }

    pub fn set_settings(&mut self, settings: Value) -> Result<Vec<(String, Vec<Value>)>, String> {
        self.settings.set(&mut self.db, settings)?;
        self.diagnostics_for_open_documents()
    }

    pub fn open_document(&mut self, uri: String, text: String) -> Result<Vec<Value>, String> {
        self.set_open_document(uri.clone(), text);
        self.diagnostics(&uri)
    }

    pub fn replace_document_text(&mut self, uri: String, text: String) {
        let Some(document) = self.documents.get_mut(&uri) else {
            self.set_open_document(uri, text);
            return;
        };
        document.replace_text(&mut self.db, text);
    }

    pub fn change_document_full(
        &mut self,
        uri: String,
        text: String,
    ) -> Result<Vec<Value>, String> {
        self.replace_document_text(uri.clone(), text);
        self.diagnostics(&uri)
    }

    pub fn edit_document_text(
        &mut self,
        uri: String,
        range: TextRange,
        text: String,
    ) -> Result<(), String> {
        let Some(document) = self.documents.get_mut(&uri) else {
            self.set_open_document(uri, text);
            return Ok(());
        };
        document.edit(&mut self.db, range, &text)
    }

    pub fn change_document_incremental(
        &mut self,
        uri: String,
        range: TextRange,
        text: String,
    ) -> Result<Vec<Value>, String> {
        self.edit_document_text(uri.clone(), range, text)?;
        self.diagnostics(&uri)
    }

    pub fn close_document(&mut self, uri: &str) {
        self.documents.remove(uri);
    }

    pub fn is_open_document(&self, uri: &str) -> bool {
        self.documents.contains_key(uri)
    }

    pub fn open_document_uris(&self) -> Vec<String> {
        self.documents.keys().cloned().collect()
    }

    pub fn replace_indexed_documents(&mut self, files: Vec<(String, String)>) {
        self.indexed_documents = files
            .into_iter()
            .filter(|(uri, _)| !self.documents.contains_key(uri))
            .map(|(uri, text)| {
                let document = OpenDocument::new(&self.db, uri.clone(), text);
                (uri, document)
            })
            .collect();
    }

    pub fn clear_process_cache(&mut self) {
        let open_documents = self
            .documents
            .iter()
            .map(|(uri, document)| (uri.clone(), document.text(&self.db).clone()))
            .collect::<Vec<_>>();
        let indexed_documents = self
            .indexed_documents
            .iter()
            .map(|(uri, document)| (uri.clone(), document.text(&self.db).clone()))
            .collect::<Vec<_>>();
        let settings_json = self.settings.input.json(&self.db).clone();

        self.db = IdeDatabase::default();
        self.settings = WorkspaceSettingsState::new(&self.db);
        self.settings.input.set_json(&mut self.db).to(settings_json);
        self.documents = open_documents
            .into_iter()
            .map(|(uri, text)| {
                let document = OpenDocument::new(&self.db, uri.clone(), text);
                (uri, document)
            })
            .collect();
        self.indexed_documents = indexed_documents
            .into_iter()
            .filter(|(uri, _)| !self.documents.contains_key(uri))
            .map(|(uri, text)| {
                let document = OpenDocument::new(&self.db, uri.clone(), text);
                (uri, document)
            })
            .collect();
    }

    pub fn diagnostics(&self, uri: &str) -> Result<Vec<Value>, String> {
        let Some(document) = self.documents.get(uri) else {
            return Ok(Vec::new());
        };
        document_diagnostics(&self.db, document.source_file, self.settings.input)
    }

    pub fn workspace_diagnostics(&self, uri: &str) -> Result<Vec<Value>, String> {
        let Some(document) = self
            .documents
            .get(uri)
            .or_else(|| self.indexed_documents.get(uri))
        else {
            return Ok(Vec::new());
        };
        document_diagnostics(&self.db, document.source_file, self.settings.input)
    }

    pub fn parse_asp(&self, uri: &str) -> Result<Value, String> {
        let Some(document) = self.documents.get(uri) else {
            return Ok(Value::Null);
        };
        parse_asp(&self.db, document.source_file, self.settings.input)
    }

    pub fn parser_diagnostics(&self, uri: &str) -> Result<Vec<Value>, String> {
        let Some(document) = self.documents.get(uri) else {
            return Ok(Vec::new());
        };
        parser_diagnostics(&self.db, document.source_file, self.settings.input)
    }

    pub fn vb_symbols(&self, uri: &str) -> Result<Vec<Value>, String> {
        let Some(document) = self.documents.get(uri) else {
            return Ok(Vec::new());
        };
        vb_symbols(&self.db, document.source_file, self.settings.input)
    }

    pub fn vb_diagnostics(&self, uri: &str) -> Result<Vec<Value>, String> {
        let Some(document) = self.documents.get(uri) else {
            return Ok(Vec::new());
        };
        vb_diagnostics(&self.db, document.source_file, self.settings.input)
    }

    pub fn include_closure(&self, uri: &str) -> Result<Vec<Value>, String> {
        if !self.documents.contains_key(uri) {
            return Ok(Vec::new());
        }

        let mut closure = Vec::new();
        let mut visited = Vec::new();
        self.collect_include_closure(uri, &mut visited, &mut closure)?;
        Ok(closure)
    }

    fn direct_include_refs(&self, uri: &str) -> Result<Vec<Value>, String> {
        let Some(document) = self.documents.get(uri) else {
            return Ok(Vec::new());
        };
        include_refs(&self.db, document.source_file, self.settings.input)
    }

    fn collect_include_closure(
        &self,
        uri: &str,
        visited: &mut Vec<String>,
        closure: &mut Vec<Value>,
    ) -> Result<(), String> {
        if visited.iter().any(|visited_uri| visited_uri == uri) {
            return Ok(());
        }
        visited.push(uri.to_string());

        for include in self.direct_include_refs(uri)? {
            let next_uri = resolve_include_uri(uri, &include);
            closure.push(include);
            if let Some(next_uri) = next_uri {
                self.collect_include_closure(&next_uri, visited, closure)?;
            }
        }
        Ok(())
    }

    pub fn completion(&self, uri: &str, position: TextPosition) -> Result<Value, String> {
        let Some(context) = self.vb_context(uri, position)? else {
            return Ok(Value::Array(Vec::new()));
        };
        let prefix = identifier_prefix_at(&context.text, context.offset);
        if let Some(type_name) = member_completion_type_name(
            uri,
            &context.text,
            &context.symbols,
            context.offset,
            &prefix,
        ) {
            return Ok(Value::Array(builtin_member_completion_items(
                type_name, &prefix,
            )));
        }
        let mut items = Vec::new();
        for symbol in &context.symbols {
            let Some(name) = symbol.get("name").and_then(Value::as_str) else {
                continue;
            };
            if !prefix.is_empty() && !name.to_lowercase().starts_with(&prefix.to_lowercase()) {
                continue;
            }
            items.push(serde_json::json!({
                "label": name,
                "kind": completion_kind(symbol),
                "detail": symbol_detail(symbol),
                "data": { "kind": "vbscript", "uri": uri },
            }));
        }
        items.extend(builtin_completion_items(&prefix));
        for keyword in [
            "Dim", "Function", "Sub", "If", "Then", "Else", "End If", "For", "Next",
        ] {
            if prefix.is_empty() || keyword.to_lowercase().starts_with(&prefix.to_lowercase()) {
                items.push(serde_json::json!({ "label": keyword, "kind": 14 }));
            }
        }
        Ok(Value::Array(items))
    }

    pub fn resolve_completion_item(&self, item: Value) -> Result<Value, String> {
        let Some(label) = item.get("label").and_then(Value::as_str) else {
            return Ok(item);
        };
        let Some(object) = item.as_object() else {
            return Ok(item);
        };
        let builtin = item
            .pointer("/data/owner")
            .and_then(Value::as_str)
            .and_then(|owner| builtin_member_completion_detail(owner, label))
            .or_else(|| builtin_completion_detail(label));
        let Some(builtin) = builtin else {
            return Ok(item);
        };
        let mut item = object.clone();
        if item.get("detail").is_none_or(Value::is_null) {
            item.insert("detail".to_string(), builtin.detail);
        }
        if item.get("documentation").is_none_or(Value::is_null) {
            item.insert(
                "documentation".to_string(),
                serde_json::json!({
                "kind": "markdown",
                "value": builtin.documentation,
                }),
            );
        }
        Ok(Value::Object(item))
    }

    pub fn hover(&self, uri: &str, position: TextPosition) -> Result<Value, String> {
        let Some((symbol, _context)) = self.symbol_at_position(uri, position)? else {
            return Ok(Value::Null);
        };
        let Some(name) = symbol.get("name").and_then(Value::as_str) else {
            return Ok(Value::Null);
        };
        let signature = symbol_signature(&symbol).unwrap_or_else(|| name.to_string());
        Ok(serde_json::json!({
            "contents": {
                "kind": "markdown",
                "value": format!("```vbscript\n{signature}\n```"),
            }
        }))
    }

    pub fn definition(&self, uri: &str, position: TextPosition) -> Result<Value, String> {
        let Some((symbol, _context)) = self.symbol_at_position(uri, position)? else {
            return Ok(Value::Null);
        };
        let Some(range) = symbol.get("range").cloned() else {
            return Ok(Value::Null);
        };
        let target_uri = symbol
            .get("sourceUri")
            .and_then(Value::as_str)
            .unwrap_or(uri);
        Ok(serde_json::json!({ "uri": target_uri, "range": range }))
    }

    pub fn type_definition(&self, uri: &str, position: TextPosition) -> Result<Value, String> {
        let Some((occurrence, context)) = self.identifier_context_at_position(uri, position)?
        else {
            return Ok(Value::Null);
        };
        let workspace_symbols = self.workspace_vb_symbols()?;
        let resolved_symbol = resolve_semantic_symbol(
            uri,
            &context.text,
            &workspace_symbols,
            &occurrence,
        )
        .or_else(|| {
            resolve_symbol_for_identifier(uri, &context.text, &workspace_symbols, &occurrence)
        });
        let type_name = resolved_symbol
            .and_then(class_type_name_from_symbol)
            .or_else(|| resolved_symbol.and_then(member_owner_name_from_symbol))
            .or_else(|| {
                member_access_owner_type_name(uri, &context.text, &workspace_symbols, &occurrence)
            })
            .or_else(|| {
                member_access_subject_type_name(
                    uri,
                    &context.text,
                    &workspace_symbols,
                    &occurrence,
                    context.offset,
                )
            });
        let Some(class_symbol) =
            type_name.and_then(|type_name| resolve_class_symbol(&workspace_symbols, type_name))
        else {
            return Ok(Value::Null);
        };
        let Some(range) = class_symbol.get("range").cloned() else {
            return Ok(Value::Null);
        };
        let target_uri = class_symbol
            .get("sourceUri")
            .and_then(Value::as_str)
            .unwrap_or(uri);
        Ok(serde_json::json!({ "uri": target_uri, "range": range }))
    }

    pub fn signature_help(&self, uri: &str, position: TextPosition) -> Result<Value, String> {
        let Some(context) = self.vb_context(uri, position)? else {
            return Ok(Value::Null);
        };
        let Some(name) = call_name_before_offset(&context.text, context.offset) else {
            return Ok(Value::Null);
        };
        let Some(symbol) = context
            .symbols
            .iter()
            .find(|symbol| symbol_name_eq(symbol, &name) && is_callable_symbol(symbol))
        else {
            return Ok(Value::Null);
        };
        let label = symbol_signature(symbol).unwrap_or(name);
        let parameters = symbol
            .get("parameters")
            .and_then(Value::as_array)
            .into_iter()
            .flatten()
            .filter_map(Value::as_str)
            .map(|label| serde_json::json!({ "label": label }))
            .collect::<Vec<_>>();
        Ok(serde_json::json!({
            "signatures": [{ "label": label, "parameters": parameters }],
            "activeSignature": 0,
            "activeParameter": active_parameter(&context.text, context.offset),
        }))
    }

    pub fn document_symbols(&self, uri: &str) -> Result<Value, String> {
        let Some(document) = self.documents.get(uri) else {
            return Ok(Value::Array(Vec::new()));
        };
        let symbols = vb_symbols(&self.db, document.source_file, self.settings.input)?;
        let items = symbols
            .into_iter()
            .filter(|symbol| {
                symbol
                    .get("sourceUri")
                    .and_then(Value::as_str)
                    .map_or(true, |source_uri| source_uri == uri)
            })
            .filter(|symbol| {
                matches!(
                    symbol.get("kind").and_then(Value::as_str),
                    Some("function" | "sub" | "class" | "method" | "property")
                )
            })
            .filter_map(|symbol| {
                let name = symbol.get("name")?.as_str()?;
                let display_name = symbol
                    .get("memberOf")
                    .and_then(Value::as_str)
                    .map(|owner| format!("{owner}.{name}"))
                    .unwrap_or_else(|| name.to_string());
                let range = symbol.get("range")?.clone();
                Some(serde_json::json!({
                    "name": display_name,
                    "kind": document_symbol_kind(&symbol),
                    "range": range,
                    "selectionRange": range,
                }))
            })
            .collect::<Vec<_>>();
        Ok(Value::Array(items))
    }

    pub fn folding_ranges(&self, uri: &str) -> Result<Value, String> {
        let Some(document) = self.documents.get(uri) else {
            return Ok(Value::Array(Vec::new()));
        };
        let ranges = vb_symbols(&self.db, document.source_file, self.settings.input)?
            .into_iter()
            .filter_map(|symbol| {
                symbol
                    .get("scopeRange")
                    .and_then(folding_range_from_lsp_range)
            })
            .collect::<Vec<_>>();
        Ok(Value::Array(ranges))
    }

    pub fn document_highlights(&self, uri: &str, position: TextPosition) -> Result<Value, String> {
        let Some((symbol, context)) = self.symbol_at_position(uri, position)? else {
            return Ok(Value::Array(Vec::new()));
        };
        let Some(name) = symbol.get("name").and_then(Value::as_str) else {
            return Ok(Value::Array(Vec::new()));
        };
        let highlights = identifier_ranges(&context.text, &context.parsed, name)
            .into_iter()
            .map(|range| serde_json::json!({ "range": range, "kind": 1 }))
            .collect::<Vec<_>>();
        Ok(Value::Array(highlights))
    }

    pub fn references(
        &self,
        uri: &str,
        position: TextPosition,
        include_declaration: bool,
    ) -> Result<Value, String> {
        let Some((symbol, _context)) = self.symbol_at_position(uri, position)? else {
            return Ok(Value::Array(Vec::new()));
        };
        let workspace_symbols = self.workspace_vb_symbols()?;
        let mut references = Vec::new();
        for (document_uri, document) in self.workspace_documents() {
            let text = document.text(&self.db);
            let parsed = parse_asp(&self.db, document.source_file, self.settings.input)?;
            for range in
                symbol_identifier_ranges(document_uri, text, &parsed, &workspace_symbols, &symbol)
            {
                if !include_declaration
                    && is_symbol_declaration_range(&symbol, document_uri, &range)
                {
                    continue;
                }
                references.push(serde_json::json!({
                    "uri": document_uri,
                    "range": range,
                }));
            }
        }
        Ok(Value::Array(references))
    }

    pub fn prepare_rename(&self, uri: &str, position: TextPosition) -> Result<Value, String> {
        let Some((_symbol, context)) = self.symbol_at_position(uri, position)? else {
            return Ok(Value::Null);
        };
        Ok(identifier_range_at_offset(&context.text, context.offset).unwrap_or(Value::Null))
    }

    pub fn rename(
        &self,
        uri: &str,
        position: TextPosition,
        new_name: &str,
    ) -> Result<Value, String> {
        if !valid_vb_identifier(new_name) {
            return Ok(Value::Null);
        }
        let Some((symbol, _context)) = self.symbol_at_position(uri, position)? else {
            return Ok(Value::Null);
        };
        let workspace_symbols = self.workspace_vb_symbols()?;
        let mut changes = serde_json::Map::new();
        for (document_uri, document) in self.workspace_documents() {
            let text = document.text(&self.db);
            let parsed = parse_asp(&self.db, document.source_file, self.settings.input)?;
            let edits =
                symbol_identifier_ranges(document_uri, text, &parsed, &workspace_symbols, &symbol)
                    .into_iter()
                    .map(|range| serde_json::json!({ "range": range, "newText": new_name }))
                    .collect::<Vec<_>>();
            if !edits.is_empty() {
                changes.insert(document_uri.clone(), Value::Array(edits));
            }
        }
        Ok(serde_json::json!({ "changes": changes }))
    }

    pub fn workspace_symbols(&self, query: &str) -> Result<Value, String> {
        let query = query.to_lowercase();
        let mut items = Vec::new();
        for (uri, document) in self.workspace_documents() {
            for symbol in vb_symbols(&self.db, document.source_file, self.settings.input)? {
                let Some(name) = symbol.get("name").and_then(Value::as_str) else {
                    continue;
                };
                if !query.is_empty() && !name.to_lowercase().contains(&query) {
                    continue;
                }
                let Some(range) = symbol.get("range").cloned() else {
                    continue;
                };
                items.push(serde_json::json!({
                    "name": name,
                    "kind": symbol_kind(&symbol),
                    "location": { "uri": uri, "range": range },
                }));
            }
        }
        Ok(Value::Array(items))
    }

    pub fn semantic_tokens(&self, uri: &str, range: Option<TextRange>) -> Result<Value, String> {
        let Some(document) = self.documents.get(uri) else {
            return Ok(serde_json::json!({ "data": [] }));
        };
        let text = document.text(&self.db);
        let parsed = parse_asp(&self.db, document.source_file, self.settings.input)?;
        let symbols = self.workspace_vb_symbols()?;
        let range_offsets = range.and_then(|range| {
            Some((
                position_to_utf16_offset(
                    text,
                    range.start.line.try_into().ok()?,
                    range.start.character.try_into().ok()?,
                )?,
                position_to_utf16_offset(
                    text,
                    range.end.line.try_into().ok()?,
                    range.end.character.try_into().ok()?,
                )?,
            ))
        });
        let mut tokens = Vec::new();
        tokens.extend(asp_delimiter_semantic_tokens(text, &parsed));
        tokens.extend(include_semantic_tokens(text, &parsed));
        tokens.extend(operator_semantic_tokens(text, &parsed));
        for identifier in identifiers_in_vbscript(text, &parsed) {
            if let Some((start, end)) = range_offsets {
                if identifier.start < start || identifier.end > end {
                    continue;
                }
            }
            if is_classic_asp_object_name(&identifier.name) {
                tokens.push(SemanticToken {
                    range: identifier.range,
                    token_type: 1,
                    token_modifiers: 1 << 3,
                });
                continue;
            }
            let Some((token_type, token_modifiers)) =
                resolve_semantic_symbol(uri, text, &symbols, &identifier)
                    .and_then(semantic_symbol_kind)
                    .or_else(|| builtin_semantic_token(text, &identifier, &parsed))
            else {
                continue;
            };
            tokens.push(SemanticToken {
                range: identifier.range,
                token_type,
                token_modifiers,
            });
        }
        if let Some((start, end)) = range_offsets {
            tokens.retain(|token| {
                range_start_offset(text, &token.range)
                    .is_some_and(|offset| offset >= start && offset < end)
            });
        }
        tokens.sort_by_key(|token| {
            (
                token
                    .range
                    .pointer("/start/line")
                    .and_then(Value::as_u64)
                    .unwrap_or(0),
                token
                    .range
                    .pointer("/start/character")
                    .and_then(Value::as_u64)
                    .unwrap_or(0),
            )
        });
        Ok(serde_json::json!({ "data": encode_semantic_tokens(&tokens) }))
    }

    pub fn document_links(&self, uri: &str) -> Result<Value, String> {
        let Some(document) = self.documents.get(uri) else {
            return Ok(Value::Array(Vec::new()));
        };
        let parsed = parse_asp(&self.db, document.source_file, self.settings.input)?;
        let links = parsed
            .get("includes")
            .and_then(Value::as_array)
            .into_iter()
            .flatten()
            .filter_map(|include| {
                Some(serde_json::json!({
                    "range": include.get("pathRange")?.clone(),
                    "target": resolve_include_target(uri, include),
                }))
            })
            .collect::<Vec<_>>();
        Ok(Value::Array(links))
    }

    pub fn selection_ranges(&self, uri: &str, positions: &[TextPosition]) -> Result<Value, String> {
        let Some(document) = self.documents.get(uri) else {
            return Ok(Value::Array(Vec::new()));
        };
        let text = document.text(&self.db);
        let ranges = positions
            .iter()
            .map(|position| {
                let offset = position_to_utf16_offset(
                    text,
                    position.line.try_into().unwrap_or(0),
                    position.character.try_into().unwrap_or(0),
                );
                let range = offset
                    .and_then(|offset| identifier_range_at_offset(text, offset))
                    .unwrap_or_else(|| {
                        serde_json::json!({
                            "start": { "line": position.line, "character": position.character },
                            "end": { "line": position.line, "character": position.character },
                        })
                    });
                let parent = line_range(text, position.line).unwrap_or_else(|| range.clone());
                serde_json::json!({ "range": range, "parent": { "range": parent } })
            })
            .collect::<Vec<_>>();
        Ok(Value::Array(ranges))
    }

    pub fn inlay_hints(
        &self,
        uri: &str,
        range: TextRange,
        settings: &Value,
    ) -> Result<Value, String> {
        let Some(document) = self.documents.get(uri) else {
            return Ok(Value::Array(Vec::new()));
        };
        let text = document.text(&self.db);
        let parsed = parse_asp(&self.db, document.source_file, self.settings.input)?;
        let symbols = vb_symbols(&self.db, document.source_file, self.settings.input)?;
        let options = InlayHintOptions::from_settings(settings);
        let start_offset = position_to_utf16_offset(
            text,
            range.start.line.try_into().unwrap_or(0),
            range.start.character.try_into().unwrap_or(0),
        )
        .unwrap_or(0);
        let end_offset = position_to_utf16_offset(
            text,
            range.end.line.try_into().unwrap_or(usize::MAX),
            range.end.character.try_into().unwrap_or(usize::MAX),
        )
        .unwrap_or_else(|| utf16_len(text));
        let mut hints = Vec::new();

        if options.variable_types {
            for symbol in symbols
                .iter()
                .filter(|symbol| symbol_source_uri_matches(symbol, uri))
                .filter(|symbol| {
                    matches!(
                        symbol.get("kind").and_then(Value::as_str),
                        Some("variable" | "constant" | "field")
                    )
                })
            {
                let Some(type_name) = visible_inlay_type_name(symbol) else {
                    continue;
                };
                let Some(range) = symbol.get("range") else {
                    continue;
                };
                if !range_overlaps_offsets(text, range, start_offset, end_offset) {
                    continue;
                }
                let Some(position) = variable_type_hint_position(text, symbol) else {
                    continue;
                };
                hints.push(serde_json::json!({
                    "position": position,
                    "label": format!("{} As {type_name}", scope_inlay_prefix(symbol, &options.global_variable_markers)),
                    "kind": 1,
                    "paddingLeft": false,
                    "paddingRight": true,
                    "tooltip": "Inferred VBScript type",
                }));
            }
        }

        if options.function_return_types {
            for symbol in symbols
                .iter()
                .filter(|symbol| symbol_source_uri_matches(symbol, uri))
                .filter(|symbol| {
                    matches!(
                        symbol.get("kind").and_then(Value::as_str),
                        Some("function" | "property")
                    )
                })
            {
                let Some(type_name) = visible_inlay_type_name(symbol) else {
                    continue;
                };
                let Some(range) = symbol.get("range") else {
                    continue;
                };
                if !range_overlaps_offsets(text, range, start_offset, end_offset) {
                    continue;
                }
                let Some(position) = function_return_hint_position(text, symbol) else {
                    continue;
                };
                hints.push(serde_json::json!({
                    "position": position,
                    "label": format!(" As {type_name}"),
                    "kind": 1,
                    "paddingLeft": false,
                    "paddingRight": true,
                    "tooltip": "Inferred VBScript return type",
                }));
            }
        }

        if options.implicit_by_ref {
            for symbol in symbols
                .iter()
                .filter(|symbol| symbol_source_uri_matches(symbol, uri))
                .filter(|symbol| symbol.get("kind").and_then(Value::as_str) == Some("parameter"))
                .filter(|symbol| {
                    symbol.get("parameterMode").and_then(Value::as_str) == Some("byref")
                })
            {
                let Some(range) = symbol.get("range") else {
                    continue;
                };
                let Some(start) = range_start_offset(text, range) else {
                    continue;
                };
                if start < start_offset || start > end_offset {
                    continue;
                }
                if has_explicit_parameter_mode_before(text, start) {
                    continue;
                }
                let Some((line, character)) = utf16_position_at(text, start) else {
                    continue;
                };
                hints.push(serde_json::json!({
                    "position": { "line": line, "character": character },
                    "label": "ByRef ",
                    "kind": 2,
                    "paddingRight": false,
                    "tooltip": "Implicit VBScript ByRef parameter",
                }));
            }
        }

        if options.parameter_names {
            for symbol in symbols.iter().filter(|symbol| is_callable_symbol(symbol)) {
                let Some(name) = symbol.get("name").and_then(Value::as_str) else {
                    continue;
                };
                let parameters = symbol
                    .get("parameters")
                    .and_then(Value::as_array)
                    .into_iter()
                    .flatten()
                    .filter_map(Value::as_str)
                    .collect::<Vec<_>>();
                if parameters.is_empty() {
                    continue;
                }
                for occurrence in identifiers_in_vbscript(text, &parsed)
                    .into_iter()
                    .filter(|identifier| identifier.name.eq_ignore_ascii_case(name))
                {
                    if occurrence.start < start_offset || occurrence.end > end_offset {
                        continue;
                    }
                    let Some(open_offset) = next_non_whitespace_offset(text, occurrence.end, '(')
                    else {
                        continue;
                    };
                    if let Some((line, character)) = utf16_position_at(text, open_offset + 1) {
                        hints.push(serde_json::json!({
                            "position": { "line": line, "character": character },
                            "label": format!("{}:", parameters[0]),
                            "kind": 2,
                            "paddingRight": true,
                        }));
                    }
                }
            }
        }

        hints.sort_by_key(|hint| {
            (
                hint.pointer("/position/line")
                    .and_then(Value::as_u64)
                    .unwrap_or(0),
                hint.pointer("/position/character")
                    .and_then(Value::as_u64)
                    .unwrap_or(0),
            )
        });
        Ok(Value::Array(hints))
    }

    pub fn code_lenses(&self, uri: &str) -> Result<Value, String> {
        let Some(document) = self.documents.get(uri) else {
            return Ok(Value::Array(Vec::new()));
        };
        let parsed = parse_asp(&self.db, document.source_file, self.settings.input)?;
        let mut lenses = Vec::new();
        for symbol in vb_symbols(&self.db, document.source_file, self.settings.input)? {
            if matches!(
                symbol.get("kind").and_then(Value::as_str),
                Some("function" | "sub" | "class" | "method" | "property")
            ) {
                let Some(range) = symbol.get("range").cloned() else {
                    continue;
                };
                lenses.push(serde_json::json!({
                    "range": range,
                    "data": {
                        "kind": "vbscript-reference",
                        "uri": uri,
                        "name": symbol.get("name").cloned().unwrap_or(Value::Null),
                        "symbolKind": symbol.get("kind").cloned().unwrap_or(Value::Null),
                        "line": symbol.pointer("/range/start/line").cloned().unwrap_or(Value::Null),
                        "character": symbol.pointer("/range/start/character").cloned().unwrap_or(Value::Null),
                    },
                }));
            }
        }
        for include in parsed
            .get("includes")
            .and_then(Value::as_array)
            .into_iter()
            .flatten()
        {
            if let Some(range) = include.get("range").cloned() {
                lenses.push(serde_json::json!({
                    "range": range,
                    "command": {
                        "title": "Open include",
                        "command": "vscode.open",
                        "arguments": [resolve_include_target(uri, include)],
                    },
                }));
            }
        }
        Ok(Value::Array(lenses))
    }

    pub fn code_actions(&self, uri: &str, range: TextRange) -> Result<Value, String> {
        let Some(document) = self.documents.get(uri) else {
            return Ok(Value::Array(Vec::new()));
        };
        let text = document.text(&self.db);
        let Some(start) = position_to_utf16_offset(
            text,
            range.start.line.try_into().unwrap_or(0),
            range.start.character.try_into().unwrap_or(0),
        ) else {
            return Ok(Value::Array(Vec::new()));
        };
        let Some(end) = position_to_utf16_offset(
            text,
            range.end.line.try_into().unwrap_or(0),
            range.end.character.try_into().unwrap_or(0),
        ) else {
            return Ok(Value::Array(Vec::new()));
        };
        let parsed = parse_asp(&self.db, document.source_file, self.settings.input)?;
        let mut actions = Vec::new();

        if is_vbscript_offset(&parsed, start) {
            let symbols = vb_symbols(&self.db, document.source_file, self.settings.input)?;
            if let Some(action) = documentation_code_action(uri, text, &symbols, start) {
                actions.push(action);
            }
        }

        if start < end
            && is_vbscript_offset(&parsed, start)
            && is_vbscript_offset(&parsed, end.saturating_sub(1))
        {
            let Ok(selected) = slice_utf16(text, start, end) else {
                return Ok(Value::Array(actions));
            };
            if !selected.trim().is_empty() && selected == selected.trim() {
                actions.push(serde_json::json!({
                    "title": "Extract variable",
                    "kind": "refactor.extract",
                    "edit": {
                        "changes": {
                            uri: [
                                {
                                    "range": {
                                        "start": { "line": range.start.line, "character": 0 },
                                        "end": { "line": range.start.line, "character": 0 },
                                    },
                                    "newText": format!("Dim extractedValue\nextractedValue = {selected}\n"),
                                },
                                {
                                    "range": {
                                        "start": {
                                            "line": range.start.line,
                                            "character": range.start.character,
                                        },
                                        "end": {
                                            "line": range.end.line,
                                            "character": range.end.character,
                                        },
                                    },
                                    "newText": "extractedValue",
                                },
                            ],
                        },
                    },
                }));
            }
        }
        Ok(Value::Array(actions))
    }

    pub fn call_hierarchy_item(&self, uri: &str, position: TextPosition) -> Result<Value, String> {
        self.hierarchy_item_matching(uri, position, is_callable_symbol)
    }

    pub fn type_hierarchy_item(&self, uri: &str, position: TextPosition) -> Result<Value, String> {
        self.hierarchy_item_matching(uri, position, is_type_hierarchy_symbol)
    }

    fn hierarchy_item_matching(
        &self,
        uri: &str,
        position: TextPosition,
        predicate: impl Fn(&Value) -> bool,
    ) -> Result<Value, String> {
        let Some((symbol, _context)) = self.symbol_at_position(uri, position)? else {
            return Ok(Value::Array(Vec::new()));
        };
        if !predicate(&symbol) {
            return Ok(Value::Array(Vec::new()));
        }
        Ok(hierarchy_item_from_symbol(uri, &symbol)
            .map(|item| Value::Array(vec![item]))
            .unwrap_or_else(|| Value::Array(Vec::new())))
    }

    pub fn call_hierarchy_incoming(&self, item: &Value) -> Result<Value, String> {
        let workspace_symbols = self.workspace_vb_symbols()?;
        let Some(target) = call_hierarchy_target_symbol(item, &workspace_symbols) else {
            return Ok(Value::Array(Vec::new()));
        };
        let mut calls = Vec::new();
        for (document_uri, document) in self.workspace_documents() {
            let text = document.text(&self.db);
            let parsed = parse_asp(&self.db, document.source_file, self.settings.input)?;
            let symbols = vb_symbols(&self.db, document.source_file, self.settings.input)?;
            for call in call_sites_in_vbscript(text, &parsed) {
                let Some(resolved) =
                    resolve_call_target_symbol(document_uri, text, &workspace_symbols, &call)
                else {
                    continue;
                };
                if !same_symbol(resolved, target) {
                    continue;
                };
                if is_symbol_declaration_range(resolved, document_uri, &call.member.range) {
                    continue;
                }
                let Some(caller) = enclosing_callable_symbol(text, &symbols, call.offset) else {
                    continue;
                };
                let Some(from) = hierarchy_item_from_symbol(document_uri, caller) else {
                    continue;
                };
                merge_call_hierarchy_entry(&mut calls, "from", from, call.range);
            }
        }
        Ok(Value::Array(calls))
    }

    pub fn call_hierarchy_outgoing(&self, item: &Value) -> Result<Value, String> {
        let workspace_symbols = self.workspace_vb_symbols()?;
        let Some(source) = call_hierarchy_target_symbol(item, &workspace_symbols) else {
            return Ok(Value::Array(Vec::new()));
        };
        let Some(item_uri) = source.get("sourceUri").and_then(Value::as_str) else {
            return Ok(Value::Array(Vec::new()));
        };
        let Some(document) = self.documents.get(item_uri) else {
            return Ok(Value::Array(Vec::new()));
        };
        let text = document.text(&self.db);
        let parsed = parse_asp(&self.db, document.source_file, self.settings.input)?;
        let Some(scope) = source.get("scopeRange") else {
            return Ok(Value::Array(Vec::new()));
        };
        let Some(scope_start) = range_start_offset(text, scope) else {
            return Ok(Value::Array(Vec::new()));
        };
        let Some(scope_end) = range_end_offset(text, scope) else {
            return Ok(Value::Array(Vec::new()));
        };
        let mut calls = Vec::new();
        for call in call_sites_in_vbscript(text, &parsed) {
            if call.offset < scope_start || call.offset > scope_end {
                continue;
            }
            let Some(callee) =
                resolve_call_target_symbol(item_uri, text, &workspace_symbols, &call)
            else {
                continue;
            };
            if same_symbol(callee, source) || !is_callable_symbol(callee) {
                continue;
            }
            if is_symbol_declaration_range(callee, item_uri, &call.member.range) {
                continue;
            }
            let Some(to) = hierarchy_item_from_symbol(item_uri, callee) else {
                continue;
            };
            merge_call_hierarchy_entry(&mut calls, "to", to, call.range);
        }
        Ok(Value::Array(calls))
    }

    pub fn type_hierarchy_relations(&self) -> Value {
        Value::Array(Vec::new())
    }

    pub fn monikers(&self, uri: &str, position: TextPosition) -> Result<Value, String> {
        let Some((symbol, _context)) = self.symbol_at_position(uri, position)? else {
            return Ok(Value::Array(Vec::new()));
        };
        let Some(name) = symbol.get("name").and_then(Value::as_str) else {
            return Ok(Value::Array(Vec::new()));
        };
        Ok(serde_json::json!([{
            "scheme": "asp-lsp",
            "identifier": format!("{uri}#{name}"),
            "unique": "document",
            "kind": "export",
        }]))
    }

    pub fn inline_values(&self, uri: &str, range: TextRange) -> Result<Value, String> {
        let Some(document) = self.documents.get(uri) else {
            return Ok(Value::Array(Vec::new()));
        };
        let text = document.text(&self.db);
        let parsed = parse_asp(&self.db, document.source_file, self.settings.input)?;
        let start_offset = position_to_utf16_offset(
            text,
            range.start.line.try_into().unwrap_or(0),
            range.start.character.try_into().unwrap_or(0),
        )
        .unwrap_or(0);
        let end_offset = position_to_utf16_offset(
            text,
            range.end.line.try_into().unwrap_or(usize::MAX),
            range.end.character.try_into().unwrap_or(usize::MAX),
        )
        .unwrap_or_else(|| utf16_len(text));
        let values = identifiers_in_vbscript(text, &parsed)
            .into_iter()
            .filter(|identifier| identifier.start >= start_offset && identifier.end <= end_offset)
            .map(|identifier| {
                serde_json::json!({
                    "range": identifier.range,
                    "variableName": identifier.name,
                    "caseSensitiveLookup": false,
                })
            })
            .collect::<Vec<_>>();
        Ok(Value::Array(values))
    }

    pub fn formatting_edits(
        &self,
        uri: &str,
        range: Option<TextRange>,
        lsp_options: &Value,
        settings: &Value,
    ) -> Result<Value, String> {
        let Some(document) = self.documents.get(uri) else {
            return Ok(Value::Array(Vec::new()));
        };
        let text = document.text(&self.db);
        let parsed = parse_asp(&self.db, document.source_file, self.settings.input)?;
        let options = FormattingOptions::from_values(lsp_options, settings);
        let (start, end) = if let Some(range) = range {
            let start = position_to_utf16_offset(
                text,
                range.start.line.try_into().unwrap_or(0),
                range.start.character.try_into().unwrap_or(0),
            )
            .map(|offset| line_start_offset(text, offset))
            .unwrap_or(0);
            let end = position_to_utf16_offset(
                text,
                range.end.line.try_into().unwrap_or(usize::MAX),
                range.end.character.try_into().unwrap_or(usize::MAX),
            )
            .map(|offset| line_end_offset(text, offset))
            .unwrap_or_else(|| utf16_len(text));
            (start, end)
        } else {
            (0, utf16_len(text))
        };
        let formatted = format_text(text, &parsed, &options, start, end)?;
        let original = slice_utf16(text, start, end)?;
        if formatted == original {
            return Ok(Value::Array(Vec::new()));
        }
        Ok(serde_json::json!([{
            "range": range_from_offsets(text, start, end).unwrap_or_else(|| serde_json::json!({
                "start": { "line": 0, "character": 0 },
                "end": { "line": 0, "character": 0 },
            })),
            "newText": formatted,
        }]))
    }

    pub fn embedded_virtual_documents(
        &self,
        uri: &str,
    ) -> Result<Vec<MappedVirtualDocument>, String> {
        let Some(document) = self.documents.get(uri) else {
            return Ok(Vec::new());
        };
        let parsed = parse_asp(&self.db, document.source_file, self.settings.input)?;
        build_virtual_documents(
            source_file_uri(&self.db, document),
            document.text(&self.db),
            &parsed,
        )
    }

    pub fn embedded_virtual_document_at(
        &self,
        uri: &str,
        position: TextPosition,
    ) -> Result<Option<(MappedVirtualDocument, Value)>, String> {
        Ok(self
            .embedded_virtual_documents(uri)?
            .into_iter()
            .find_map(|mapped| {
                if mapped.document.language_id == "vbscript" {
                    return None;
                }
                let position = mapped.virtual_position_for_source_position(position)?;
                Some((mapped, position))
            }))
    }

    fn vb_context(&self, uri: &str, position: TextPosition) -> Result<Option<VbContext>, String> {
        let Some(document) = self.documents.get(uri) else {
            return Ok(None);
        };
        let text = document.text(&self.db).clone();
        let offset = position_to_utf16_offset(
            &text,
            usize::try_from(position.line).map_err(|_| "line is too large".to_string())?,
            usize::try_from(position.character)
                .map_err(|_| "character is too large".to_string())?,
        )
        .ok_or_else(|| "position is out of bounds".to_string())?;
        let parsed = parse_asp(&self.db, document.source_file, self.settings.input)?;
        if !is_vbscript_offset(&parsed, offset) {
            return Ok(None);
        }
        let symbols = vb_symbols(&self.db, document.source_file, self.settings.input)?;
        Ok(Some(VbContext {
            text,
            offset,
            parsed,
            symbols,
        }))
    }

    fn symbol_at_position(
        &self,
        uri: &str,
        position: TextPosition,
    ) -> Result<Option<(Value, VbContext)>, String> {
        let Some((occurrence, context)) = self.identifier_context_at_position(uri, position)?
        else {
            return Ok(None);
        };
        let workspace_symbols = self.workspace_vb_symbols()?;
        let symbol =
            resolve_symbol_for_identifier(uri, &context.text, &workspace_symbols, &occurrence)
                .cloned()
                .or_else(|| {
                    self.workspace_symbol_by_name(&occurrence.name)
                        .ok()
                        .flatten()
                });
        Ok(symbol.map(|symbol| (symbol, context)))
    }

    fn identifier_context_at_position(
        &self,
        uri: &str,
        position: TextPosition,
    ) -> Result<Option<(IdentifierOccurrence, VbContext)>, String> {
        let Some(context) = self.vb_context(uri, position)? else {
            return Ok(None);
        };
        let Some(word) = identifier_at_offset(&context.text, context.offset) else {
            return Ok(None);
        };
        let Some((start, end)) = identifier_offsets_at(&context.text, context.offset) else {
            return Ok(None);
        };
        let Some(range) = range_from_offsets(&context.text, start, end) else {
            return Ok(None);
        };
        Ok(Some((
            IdentifierOccurrence {
                name: word,
                start,
                end,
                range,
            },
            context,
        )))
    }

    fn workspace_symbol_by_name(&self, name: &str) -> Result<Option<Value>, String> {
        for (_uri, document) in self.workspace_documents() {
            if let Some(symbol) = vb_symbols(&self.db, document.source_file, self.settings.input)?
                .into_iter()
                .find(|symbol| symbol_name_eq(symbol, name))
            {
                return Ok(Some(symbol));
            }
        }
        Ok(None)
    }

    fn workspace_vb_symbols(&self) -> Result<Vec<Value>, String> {
        let mut symbols = Vec::new();
        for (_uri, document) in self.workspace_documents() {
            symbols.extend(vb_symbols(
                &self.db,
                document.source_file,
                self.settings.input,
            )?);
        }
        Ok(symbols)
    }

    fn workspace_documents(&self) -> Vec<(&String, &OpenDocument)> {
        let mut documents = self.documents.iter().collect::<Vec<_>>();
        documents.extend(
            self.indexed_documents
                .iter()
                .filter(|(uri, _)| !self.documents.contains_key(*uri)),
        );
        documents
    }

    pub fn diagnostics_for_open_documents(&self) -> Result<Vec<(String, Vec<Value>)>, String> {
        self.documents
            .iter()
            .map(|(uri, document)| {
                document_diagnostics(&self.db, document.source_file, self.settings.input)
                    .map(|diagnostics| (uri.clone(), diagnostics))
            })
            .collect()
    }

    pub fn backend_status(&self) -> Value {
        asp_analysis::backend_status()
    }
}

#[derive(Clone, Copy, Debug)]
pub struct TextPosition {
    pub line: u32,
    pub character: u32,
}

#[derive(Clone, Copy, Debug)]
pub struct TextRange {
    pub start: TextPosition,
    pub end: TextPosition,
}

struct InlayHintOptions {
    variable_types: bool,
    parameter_names: bool,
    function_return_types: bool,
    implicit_by_ref: bool,
    global_variable_markers: String,
}

impl InlayHintOptions {
    fn from_settings(settings: &Value) -> Self {
        let inlay_hints = settings.get("inlayHints").unwrap_or(&Value::Null);
        Self {
            variable_types: inlay_hint_bool(inlay_hints, "variableTypes", true),
            parameter_names: inlay_hint_bool(inlay_hints, "parameterNames", true),
            function_return_types: inlay_hint_bool(inlay_hints, "functionReturnTypes", true),
            implicit_by_ref: inlay_hint_bool(inlay_hints, "implicitByRef", true),
            global_variable_markers: inlay_hints
                .get("globalVariableMarkers")
                .and_then(Value::as_str)
                .unwrap_or("global")
                .to_string(),
        }
    }
}

fn inlay_hint_bool(settings: &Value, key: &str, default: bool) -> bool {
    settings
        .get(key)
        .and_then(Value::as_bool)
        .unwrap_or(default)
}

struct OpenDocument {
    rope: Rope,
    source_file: SourceFile,
}

impl OpenDocument {
    fn new(db: &IdeDatabase, uri: String, text: String) -> Self {
        Self {
            rope: Rope::from_str(&text),
            source_file: SourceFile::new(db, uri, text),
        }
    }

    fn replace_text(&mut self, db: &mut IdeDatabase, text: String) {
        self.rope = Rope::from_str(&text);
        self.source_file.set_text(db).to(text);
    }

    fn text<'db>(&self, db: &'db IdeDatabase) -> &'db String {
        self.source_file.text(db)
    }

    fn edit(&mut self, db: &mut IdeDatabase, range: TextRange, text: &str) -> Result<(), String> {
        let start = self.position_to_char(range.start)?;
        let end = self.position_to_char(range.end)?;
        if start > end {
            return Err("range start must be before range end".to_string());
        }
        self.rope.remove(start..end);
        self.rope.insert(start, text);
        self.source_file.set_text(db).to(self.rope.to_string());
        Ok(())
    }

    fn position_to_char(&self, position: TextPosition) -> Result<usize, String> {
        let line = usize::try_from(position.line).map_err(|_| "line is too large".to_string())?;
        if line >= self.rope.len_lines() {
            return Err(format!("line {line} is out of bounds"));
        }
        let line_start = self.rope.line_to_char(line);
        let line_text = self.rope.line(line).to_string();
        let character = usize::try_from(position.character)
            .map_err(|_| "character is too large".to_string())?;
        Ok(line_start + utf16_character_to_char_offset(&line_text, character)?)
    }
}

fn source_file_uri(db: &IdeDatabase, document: &OpenDocument) -> String {
    document.source_file.uri(db).clone()
}

struct VbContext {
    text: String,
    offset: usize,
    parsed: Value,
    symbols: Vec<Value>,
}

struct IdentifierOccurrence {
    name: String,
    start: usize,
    end: usize,
    range: Value,
}

struct CallSite {
    owner: Option<IdentifierOccurrence>,
    member: IdentifierOccurrence,
    offset: usize,
    range: Value,
}

struct SemanticToken {
    range: Value,
    token_type: u32,
    token_modifiers: u32,
}

fn is_vbscript_offset(parsed: &Value, offset: usize) -> bool {
    parsed
        .get("regions")
        .and_then(Value::as_array)
        .into_iter()
        .flatten()
        .any(|region| {
            region.get("language").and_then(Value::as_str) == Some("vbscript")
                && value_usize(region, "contentStart").is_ok_and(|start| offset >= start)
                && value_usize(region, "contentEnd").is_ok_and(|end| offset <= end)
        })
}

fn is_lsp_range_object(object: &serde_json::Map<String, Value>) -> bool {
    object
        .get("start")
        .and_then(|start| start.get("line"))
        .and_then(Value::as_u64)
        .is_some()
        && object
            .get("end")
            .and_then(|end| end.get("line"))
            .and_then(Value::as_u64)
            .is_some()
}

fn identifier_at_offset(text: &str, offset: usize) -> Option<String> {
    let (start, end) = identifier_offsets_at(text, offset)?;
    slice_utf16(text, start, end).ok()
}

fn identifier_range_at_offset(text: &str, offset: usize) -> Option<Value> {
    let (start, end) = identifier_offsets_at(text, offset)?;
    range_from_offsets(text, start, end)
}

fn identifier_offsets_at(text: &str, offset: usize) -> Option<(usize, usize)> {
    let chars = utf16_chars(text);
    let index = chars
        .iter()
        .position(|(char_offset, character)| {
            offset >= *char_offset && offset <= *char_offset + character.len_utf16()
        })
        .unwrap_or(chars.len());
    let mut start = index;
    while start > 0 && is_identifier_char(chars[start - 1].1) {
        start -= 1;
    }
    let mut end = index;
    while end < chars.len() && is_identifier_char(chars[end].1) {
        end += 1;
    }
    (start < end).then(|| {
        let start_offset = chars[start].0;
        let end_offset = chars
            .get(end)
            .map(|(offset, _)| *offset)
            .unwrap_or_else(|| utf16_len(text));
        (start_offset, end_offset)
    })
}

fn identifier_prefix_at(text: &str, offset: usize) -> String {
    let chars = utf16_chars(text);
    let mut index = chars
        .iter()
        .position(|(char_offset, _)| *char_offset >= offset)
        .unwrap_or(chars.len());
    while index > 0 && is_identifier_char(chars[index - 1].1) {
        index -= 1;
    }
    chars[index..]
        .iter()
        .take_while(|(char_offset, character)| {
            *char_offset < offset && is_identifier_char(*character)
        })
        .map(|(_, character)| *character)
        .collect()
}

fn previous_non_whitespace_char_before(text: &str, offset: usize) -> Option<(usize, char)> {
    utf16_chars(text)
        .into_iter()
        .filter(|(char_offset, character)| *char_offset < offset && !character.is_whitespace())
        .last()
}

fn identifier_ranges(text: &str, parsed: &Value, name: &str) -> Vec<Value> {
    let lower_name = name.to_lowercase();
    identifiers_in_vbscript(text, parsed)
        .into_iter()
        .filter(|identifier| identifier.name.to_lowercase() == lower_name)
        .map(|identifier| identifier.range)
        .collect()
}

fn call_sites_in_vbscript(text: &str, parsed: &Value) -> Vec<CallSite> {
    identifiers_in_vbscript(text, parsed)
        .into_iter()
        .filter_map(|member| {
            let offset = next_non_whitespace_offset(text, member.end, '(')?;
            let owner = member_call_owner(text, &member);
            let range = owner
                .as_ref()
                .and_then(|owner| range_from_offsets(text, owner.start, member.end))
                .unwrap_or_else(|| member.range.clone());
            Some(CallSite {
                owner,
                member,
                offset,
                range,
            })
        })
        .collect()
}

fn member_call_owner(text: &str, member: &IdentifierOccurrence) -> Option<IdentifierOccurrence> {
    let (dot_offset, '.') = previous_non_whitespace_char_before(text, member.start)? else {
        return None;
    };
    let (owner_char_offset, _) = previous_non_whitespace_char_before(text, dot_offset)?;
    let (start, end) = identifier_offsets_at(text, owner_char_offset)?;
    let range = range_from_offsets(text, start, end)?;
    let name = slice_utf16(text, start, end).ok()?.to_string();
    Some(IdentifierOccurrence {
        name,
        start,
        end,
        range,
    })
}

fn identifiers_in_vbscript(text: &str, parsed: &Value) -> Vec<IdentifierOccurrence> {
    let regions = vbscript_regions(parsed);
    let chars = utf16_chars(text);
    let mut identifiers = Vec::new();
    let mut index = 0;
    while index < chars.len() {
        let offset = chars[index].0;
        if !regions
            .iter()
            .any(|(start, end)| offset >= *start && offset < *end)
        {
            index += 1;
            continue;
        }
        if chars[index].1 == '"' {
            index += 1;
            while index < chars.len() {
                if chars[index].1 == '"' {
                    index += 1;
                    break;
                }
                index += 1;
            }
            continue;
        }
        if chars[index].1 == '\'' {
            index += 1;
            while index < chars.len() && chars[index].1 != '\n' {
                index += 1;
            }
            continue;
        }
        if !is_identifier_char(chars[index].1) {
            index += 1;
            continue;
        }
        let start = index;
        while index < chars.len() && is_identifier_char(chars[index].1) {
            index += 1;
        }
        let text_name = chars[start..index]
            .iter()
            .map(|(_, character)| *character)
            .collect::<String>();
        let start_offset = chars[start].0;
        let end_offset = chars
            .get(index)
            .map(|(offset, _)| *offset)
            .unwrap_or_else(|| utf16_len(text));
        if let Some(range) = range_from_offsets(text, start_offset, end_offset) {
            identifiers.push(IdentifierOccurrence {
                name: text_name,
                start: start_offset,
                end: end_offset,
                range,
            });
        }
    }
    identifiers
}

fn vbscript_regions(parsed: &Value) -> Vec<(usize, usize)> {
    parsed
        .get("regions")
        .and_then(Value::as_array)
        .into_iter()
        .flatten()
        .filter(|region| region.get("language").and_then(Value::as_str) == Some("vbscript"))
        .filter_map(|region| {
            Some((
                value_usize(region, "contentStart").ok()?,
                value_usize(region, "contentEnd").ok()?,
            ))
        })
        .collect()
}

fn range_from_offsets(text: &str, start_offset: usize, end_offset: usize) -> Option<Value> {
    let (start_line, start_character) = utf16_position_at(text, start_offset)?;
    let (end_line, end_character) = utf16_position_at(text, end_offset)?;
    Some(serde_json::json!({
        "start": { "line": start_line, "character": start_character },
        "end": { "line": end_line, "character": end_character },
    }))
}

fn utf16_chars(text: &str) -> Vec<(usize, char)> {
    let mut offset = 0;
    let mut chars = Vec::new();
    for character in text.chars() {
        chars.push((offset, character));
        offset += character.len_utf16();
    }
    chars
}

fn is_identifier_char(character: char) -> bool {
    character == '_' || character.is_ascii_alphanumeric()
}

fn symbol_name_eq(symbol: &Value, name: &str) -> bool {
    symbol
        .get("name")
        .and_then(Value::as_str)
        .is_some_and(|candidate| candidate.eq_ignore_ascii_case(name))
}

fn symbol_identifier_ranges(
    document_uri: &str,
    text: &str,
    parsed: &Value,
    symbols: &[Value],
    target: &Value,
) -> Vec<Value> {
    let Some(target_name) = target.get("name").and_then(Value::as_str) else {
        return Vec::new();
    };
    identifiers_in_vbscript(text, parsed)
        .into_iter()
        .filter(|identifier| identifier.name.eq_ignore_ascii_case(target_name))
        .filter(|identifier| {
            resolve_symbol_for_identifier(document_uri, text, symbols, identifier)
                .is_some_and(|symbol| same_symbol(symbol, target))
        })
        .map(|identifier| identifier.range)
        .collect()
}

fn resolve_symbol_for_identifier<'a>(
    document_uri: &str,
    text: &str,
    symbols: &'a [Value],
    identifier: &IdentifierOccurrence,
) -> Option<&'a Value> {
    if let Some(symbol) = symbols.iter().find(|symbol| {
        symbol_name_eq(symbol, &identifier.name)
            && symbol
                .get("sourceUri")
                .and_then(Value::as_str)
                .map_or(true, |source_uri| source_uri == document_uri)
            && symbol
                .get("range")
                .is_some_and(|range| same_range(range, &identifier.range))
    }) {
        return Some(symbol);
    }

    let offset = identifier.start + ((identifier.end.saturating_sub(identifier.start)) / 2);
    visible_symbol_by_name(document_uri, text, symbols, &identifier.name, offset)
}

fn resolve_call_target_symbol<'a>(
    document_uri: &str,
    text: &str,
    symbols: &'a [Value],
    call: &CallSite,
) -> Option<&'a Value> {
    if let Some(owner) = &call.owner {
        let type_name = if owner.name.eq_ignore_ascii_case("me") {
            current_class_name_at(document_uri, text, symbols, call.offset)
        } else {
            infer_variable_type_name(document_uri, text, symbols, owner, call.offset)
        }?;
        return resolve_member_symbol(symbols, &type_name, &call.member.name)
            .filter(|symbol| is_callable_symbol(symbol));
    }
    resolve_symbol_for_identifier(document_uri, text, symbols, &call.member)
        .filter(|symbol| is_callable_symbol(symbol))
}

fn resolve_semantic_symbol<'a>(
    document_uri: &str,
    text: &str,
    symbols: &'a [Value],
    identifier: &IdentifierOccurrence,
) -> Option<&'a Value> {
    if let Some(owner) = member_call_owner(text, identifier) {
        let offset = identifier.start + ((identifier.end.saturating_sub(identifier.start)) / 2);
        let type_name = if owner.name.eq_ignore_ascii_case("me") {
            current_class_name_at(document_uri, text, symbols, offset)
        } else {
            infer_variable_type_name(document_uri, text, symbols, &owner, offset)
        }?;
        return resolve_member_symbol(symbols, &type_name, &identifier.name);
    }
    resolve_symbol_for_identifier(document_uri, text, symbols, identifier)
}

fn current_class_name_at<'a>(
    document_uri: &str,
    text: &str,
    symbols: &'a [Value],
    offset: usize,
) -> Option<&'a str> {
    symbols
        .iter()
        .filter(|symbol| {
            symbol.get("kind").and_then(Value::as_str) == Some("class")
                && symbol
                    .get("sourceUri")
                    .and_then(Value::as_str)
                    .is_some_and(|source_uri| source_uri == document_uri)
                && symbol
                    .get("scopeRange")
                    .is_some_and(|range| range_contains_offset(text, range, offset))
        })
        .filter_map(|symbol| {
            let scope = symbol.get("scopeRange")?;
            let size = range_size(scope).unwrap_or(usize::MAX);
            Some((symbol.get("name")?.as_str()?, size))
        })
        .min_by_key(|(_, size)| *size)
        .map(|(name, _)| name)
}

fn infer_variable_type_name<'a>(
    document_uri: &str,
    text: &str,
    symbols: &'a [Value],
    identifier: &IdentifierOccurrence,
    offset: usize,
) -> Option<&'a str> {
    resolve_symbol_for_identifier(document_uri, text, symbols, identifier)
        .or_else(|| visible_symbol_by_name(document_uri, text, symbols, &identifier.name, offset))
        .and_then(symbol_type_name)
}

fn member_completion_type_name<'a>(
    document_uri: &str,
    text: &str,
    symbols: &'a [Value],
    offset: usize,
    prefix: &str,
) -> Option<&'a str> {
    let prefix_start = offset.saturating_sub(utf16_len(prefix));
    let (dot_offset, '.') = previous_non_whitespace_char_before(text, prefix_start)? else {
        return None;
    };
    let (owner_offset, _) = previous_non_whitespace_char_before(text, dot_offset)?;
    let (start, end) = identifier_offsets_at(text, owner_offset)?;
    let owner = slice_utf16(text, start, end).ok()?;
    if let Some(type_name) = classic_asp_builtin_type_name(&owner) {
        return Some(type_name);
    }
    let range = range_from_offsets(text, start, end)?;
    let identifier = IdentifierOccurrence {
        name: owner,
        start,
        end,
        range,
    };
    infer_variable_type_name(document_uri, text, symbols, &identifier, offset)
}

fn member_access_owner_type_name<'a>(
    document_uri: &str,
    text: &str,
    symbols: &'a [Value],
    identifier: &IdentifierOccurrence,
) -> Option<&'a str> {
    let owner = member_call_owner(text, identifier)?;
    let offset = identifier.start + ((identifier.end.saturating_sub(identifier.start)) / 2);
    if owner.name.eq_ignore_ascii_case("me") {
        current_class_name_at(document_uri, text, symbols, offset)
    } else {
        infer_variable_type_name(document_uri, text, symbols, &owner, offset)
    }
}

fn member_access_subject_type_name<'a>(
    document_uri: &str,
    text: &str,
    symbols: &'a [Value],
    identifier: &IdentifierOccurrence,
    offset: usize,
) -> Option<&'a str> {
    next_non_whitespace_offset(text, identifier.end, '.')?;
    if identifier.name.eq_ignore_ascii_case("me") {
        current_class_name_at(document_uri, text, symbols, offset)
    } else {
        infer_variable_type_name(document_uri, text, symbols, identifier, offset)
    }
}

fn symbol_type_name(symbol: &Value) -> Option<&str> {
    symbol
        .get("type")
        .and_then(class_type_name_from_type_ref)
        .or_else(|| {
            symbol
                .get("typeName")
                .and_then(Value::as_str)
                .and_then(single_type_name_without_nothing)
        })
}

fn class_type_name_from_symbol(symbol: &Value) -> Option<&str> {
    symbol_type_name(symbol)
}

fn member_owner_name_from_symbol(symbol: &Value) -> Option<&str> {
    let has_type = symbol.get("type").is_some() || symbol.get("typeName").is_some();
    (!has_type)
        .then(|| symbol.get("memberOf").and_then(Value::as_str))
        .flatten()
}

fn class_type_name_from_type_ref(type_ref: &Value) -> Option<&str> {
    if let Some(union_types) = type_ref.get("unionTypes").and_then(Value::as_array) {
        let mut types = union_types.iter().filter_map(class_type_name_from_type_ref);
        let type_name = types.next()?;
        return types.next().is_none().then_some(type_name);
    }
    type_ref
        .get("name")
        .and_then(Value::as_str)
        .and_then(single_type_name_without_nothing)
}

fn single_type_name_without_nothing(type_name: &str) -> Option<&str> {
    let mut names = type_name
        .split('|')
        .map(str::trim)
        .filter(|name| !name.is_empty() && !name.eq_ignore_ascii_case("nothing"));
    let name = names.next()?;
    names.next().is_none().then_some(name)
}

fn resolve_class_symbol<'a>(symbols: &'a [Value], type_name: &str) -> Option<&'a Value> {
    symbols.iter().find(|symbol| {
        symbol.get("kind").and_then(Value::as_str) == Some("class")
            && symbol_name_eq(symbol, type_name)
    })
}

fn resolve_member_symbol<'a>(
    symbols: &'a [Value],
    type_name: &str,
    member_name: &str,
) -> Option<&'a Value> {
    symbols.iter().find(|symbol| {
        symbol_name_eq(symbol, member_name)
            && symbol
                .get("memberOf")
                .and_then(Value::as_str)
                .is_some_and(|owner| owner.eq_ignore_ascii_case(type_name))
    })
}

fn visible_symbol_by_name<'a>(
    document_uri: &str,
    text: &str,
    symbols: &'a [Value],
    name: &str,
    offset: usize,
) -> Option<&'a Value> {
    symbols
        .iter()
        .filter(|symbol| symbol_name_eq(symbol, name))
        .filter_map(|symbol| {
            let same_document = symbol
                .get("sourceUri")
                .and_then(Value::as_str)
                .map_or(true, |source_uri| source_uri == document_uri);
            let global = symbol.get("scopeName").and_then(Value::as_str).is_none()
                && symbol.get("memberOf").and_then(Value::as_str).is_none();
            if !same_document {
                return global.then_some((symbol, 1, usize::MAX));
            }
            let scope_contains = symbol
                .get("scopeRange")
                .is_some_and(|range| range_contains_offset(text, range, offset));
            if !scope_contains && !global {
                return None;
            }
            let score = if scope_contains { 2 } else { 1 };
            let size = symbol
                .get("scopeRange")
                .and_then(range_size)
                .unwrap_or(usize::MAX);
            Some((symbol, score, size))
        })
        .max_by(|(_, left_score, left_size), (_, right_score, right_size)| {
            left_score
                .cmp(right_score)
                .then_with(|| right_size.cmp(left_size))
        })
        .map(|(symbol, _, _)| symbol)
}

fn same_symbol(left: &Value, right: &Value) -> bool {
    let Some(left_uri) = left.get("sourceUri").and_then(Value::as_str) else {
        return false;
    };
    let Some(right_uri) = right.get("sourceUri").and_then(Value::as_str) else {
        return false;
    };
    left_uri == right_uri
        && symbol_name_eq(
            left,
            right
                .get("name")
                .and_then(Value::as_str)
                .unwrap_or_default(),
        )
        && left.get("kind").and_then(Value::as_str) == right.get("kind").and_then(Value::as_str)
        && left
            .get("memberOf")
            .and_then(Value::as_str)
            .unwrap_or_default()
            .eq_ignore_ascii_case(
                right
                    .get("memberOf")
                    .and_then(Value::as_str)
                    .unwrap_or_default(),
            )
        && left
            .get("range")
            .zip(right.get("range"))
            .is_some_and(|(left_range, right_range)| same_range(left_range, right_range))
}

fn is_symbol_declaration_range(symbol: &Value, document_uri: &str, range: &Value) -> bool {
    symbol
        .get("sourceUri")
        .and_then(Value::as_str)
        .map_or(true, |source_uri| source_uri == document_uri)
        && symbol
            .get("range")
            .is_some_and(|symbol_range| same_range(symbol_range, range))
}

fn same_range(left: &Value, right: &Value) -> bool {
    left == right
}

fn range_contains_offset(text: &str, range: &Value, offset: usize) -> bool {
    let Some(start) = range_start_offset(text, range) else {
        return false;
    };
    let Some(end) = range_end_offset(text, range) else {
        return false;
    };
    start <= offset && offset <= end
}

fn range_overlaps_offsets(
    text: &str,
    range: &Value,
    start_offset: usize,
    end_offset: usize,
) -> bool {
    let Some(start) = range_start_offset(text, range) else {
        return false;
    };
    let Some(end) = range_end_offset(text, range) else {
        return false;
    };
    start <= end_offset && end >= start_offset
}

fn symbol_source_uri_matches(symbol: &Value, uri: &str) -> bool {
    symbol
        .get("sourceUri")
        .and_then(Value::as_str)
        .map_or(true, |source_uri| source_uri == uri)
}

fn documentation_code_action(
    uri: &str,
    text: &str,
    symbols: &[Value],
    offset: usize,
) -> Option<Value> {
    let symbol = documentation_symbol_at(uri, text, symbols, offset)?;
    let owner = if symbol.get("kind").and_then(Value::as_str) == Some("parameter") {
        callable_owner_for_parameter(symbol, symbols)?
    } else {
        symbol
    };
    let declaration_line = owner.pointer("/range/start/line")?.as_u64()?;
    let declaration_line = u32::try_from(declaration_line).ok()?;
    let indent = line_indent(text, declaration_line);
    let new_line = preferred_new_line(text);
    let existing_block = documentation_comment_block_before(text, declaration_line);
    let existing_block_text = existing_block
        .as_ref()
        .map(|(_, block_text)| block_text.as_str());
    let annotation_lines = documentation_annotation_lines(text, symbol, owner, symbols);
    let xml_lines = documentation_xml_lines(symbol, owner, existing_block_text);
    if annotation_lines.is_empty() && xml_lines.is_empty() {
        return None;
    }
    let (range_start_line, range_end_line, new_text) =
        if let Some((block_start_line, block_text)) = existing_block {
            let mut parts = Vec::new();
            parts.extend(
                annotation_lines
                    .iter()
                    .map(|line| format!("{indent}{line}")),
            );
            parts.push(block_text.trim_end_matches(['\r', '\n']).to_string());
            parts.extend(xml_lines.iter().map(|line| format!("{indent}{line}")));
            (
                block_start_line,
                declaration_line,
                format!("{}{}", parts.join(new_line), new_line),
            )
        } else {
            let mut lines = annotation_lines;
            lines.extend(xml_lines);
            (
                declaration_line,
                declaration_line,
                format!(
                    "{}{}",
                    lines
                        .into_iter()
                        .map(|line| format!("{indent}{line}"))
                        .collect::<Vec<_>>()
                        .join(new_line),
                    new_line
                ),
            )
        };

    Some(serde_json::json!({
        "title": "Generate VBScript documentation",
        "kind": "quickfix",
        "edit": {
            "changes": {
                uri: [{
                    "range": {
                        "start": { "line": range_start_line, "character": 0 },
                        "end": { "line": range_end_line, "character": 0 },
                    },
                    "newText": new_text,
                }],
            },
        },
    }))
}

fn documentation_symbol_at<'a>(
    uri: &str,
    text: &str,
    symbols: &'a [Value],
    offset: usize,
) -> Option<&'a Value> {
    symbols
        .iter()
        .filter(|symbol| symbol_source_uri_matches(symbol, uri))
        .filter(|symbol| is_documentation_action_symbol(symbol))
        .filter(|symbol| symbol.get("implicit").and_then(Value::as_bool) != Some(true))
        .filter(|symbol| {
            symbol
                .get("range")
                .is_some_and(|range| range_contains_offset(text, range, offset))
        })
        .min_by_key(|symbol| {
            symbol
                .get("range")
                .and_then(range_size)
                .unwrap_or(usize::MAX)
        })
}

fn is_documentation_action_symbol(symbol: &Value) -> bool {
    matches!(
        symbol.get("kind").and_then(Value::as_str),
        Some(
            "class"
                | "function"
                | "sub"
                | "method"
                | "property"
                | "variable"
                | "field"
                | "constant"
                | "parameter"
        )
    )
}

fn callable_owner_for_parameter<'a>(parameter: &Value, symbols: &'a [Value]) -> Option<&'a Value> {
    let scope = parameter.get("scopeRange")?;
    symbols.iter().find(|candidate| {
        is_callable_symbol(candidate)
            && candidate
                .get("scopeRange")
                .is_some_and(|candidate_scope| same_range(candidate_scope, scope))
            && candidate.get("sourceUri") == parameter.get("sourceUri")
    })
}

fn documentation_xml_lines(
    symbol: &Value,
    owner: &Value,
    existing_block_text: Option<&str>,
) -> Vec<String> {
    let Some(name) = symbol.get("name").and_then(Value::as_str) else {
        return Vec::new();
    };
    let mut lines = Vec::new();
    if symbol.get("kind").and_then(Value::as_str) == Some("parameter") {
        if !documentation_has_param(owner, name, existing_block_text) {
            lines.push(format!(
                "''' <param name=\"{name}\">TODO: Describe {name}.</param>"
            ));
        }
        return lines;
    }
    if !documentation_has_text(owner, "summary", existing_block_text) {
        lines.push(format!("''' <summary>TODO: Describe {name}.</summary>"));
    }
    if is_callable_symbol(symbol) {
        for parameter in documentation_parameter_names(symbol) {
            if !documentation_has_param(owner, &parameter, existing_block_text) {
                lines.push(format!(
                    "''' <param name=\"{parameter}\">TODO: Describe {parameter}.</param>"
                ));
            }
        }
        if documentation_symbol_has_return_value(symbol)
            && !documentation_has_text(owner, "returns", existing_block_text)
        {
            lines.push("''' <returns>TODO: Describe return value.</returns>".to_string());
        }
    }
    if documentation_symbol_has_value(symbol)
        && !documentation_has_text(owner, "value", existing_block_text)
    {
        lines.push(format!("''' <value>TODO: Describe {name}.</value>"));
    }
    lines
}

fn documentation_annotation_lines(
    text: &str,
    symbol: &Value,
    owner: &Value,
    symbols: &[Value],
) -> Vec<String> {
    let Some(name) = symbol.get("name").and_then(Value::as_str) else {
        return Vec::new();
    };
    let mut lines = Vec::new();
    match symbol.get("kind").and_then(Value::as_str) {
        Some("parameter") => {
            let owner_name = owner
                .get("name")
                .and_then(Value::as_str)
                .unwrap_or_default();
            let marker = format!("@param {owner_name}.{name}");
            if !contains_case_insensitive(text, &marker) {
                lines.push(format!(
                    "' @param {owner_name}.{name} As {}",
                    documentation_symbol_type_name(symbol)
                ));
            }
        }
        Some("variable" | "field" | "constant") => {
            let marker = format!("@type {name}");
            if !contains_case_insensitive(text, &marker) {
                lines.push(format!(
                    "' @type {name} As {}",
                    documentation_symbol_type_name(symbol)
                ));
            }
        }
        Some("function" | "sub" | "method" | "property") => {
            for parameter in documentation_parameter_names(symbol) {
                let marker = format!("@param {name}.{parameter}");
                if !contains_case_insensitive(text, &marker) {
                    let parameter_symbol =
                        parameter_symbol_for_callable(symbol, &parameter, symbols);
                    lines.push(format!(
                        "' @param {name}.{parameter} As {}",
                        parameter_symbol
                            .map(documentation_symbol_type_name)
                            .unwrap_or_else(|| "Variant".to_string())
                    ));
                }
            }
            let marker = format!("@returns {name}");
            if documentation_symbol_has_return_value(symbol)
                && !contains_case_insensitive(text, &marker)
            {
                lines.push(format!(
                    "' @returns {name} {}",
                    documentation_symbol_type_name(symbol)
                ));
            }
        }
        _ => {}
    }
    lines
}

fn documentation_parameter_names(symbol: &Value) -> Vec<String> {
    if let Some(parameters) = symbol.get("parameters").and_then(Value::as_array) {
        let names = parameters
            .iter()
            .filter_map(Value::as_str)
            .map(str::to_string)
            .collect::<Vec<_>>();
        if !names.is_empty() {
            return names;
        }
    }
    symbol
        .get("parameterDetails")
        .and_then(Value::as_array)
        .into_iter()
        .flatten()
        .filter_map(|parameter| parameter.get("name").and_then(Value::as_str))
        .map(str::to_string)
        .collect()
}

fn documentation_symbol_has_return_value(symbol: &Value) -> bool {
    match symbol.get("kind").and_then(Value::as_str) {
        Some("function") => true,
        Some("method") => symbol.get("procedureKind").and_then(Value::as_str) != Some("sub"),
        Some("property") => symbol.get("propertyAccessor").and_then(Value::as_str) == Some("get"),
        _ => false,
    }
}

fn documentation_symbol_has_value(symbol: &Value) -> bool {
    matches!(
        symbol.get("kind").and_then(Value::as_str),
        Some("variable" | "field" | "constant" | "property")
    )
}

fn documentation_has_text(symbol: &Value, key: &str, existing_block_text: Option<&str>) -> bool {
    symbol
        .get("documentation")
        .and_then(|documentation| documentation.get(key))
        .and_then(Value::as_str)
        .is_some_and(|text| !text.trim().is_empty())
        || existing_block_text.is_some_and(|text| {
            contains_case_insensitive(text, &format!("<{key}>"))
                && contains_case_insensitive(text, &format!("</{key}>"))
        })
}

fn documentation_has_param(symbol: &Value, name: &str, existing_block_text: Option<&str>) -> bool {
    symbol
        .pointer(&format!("/documentation/params/{name}"))
        .and_then(Value::as_str)
        .is_some_and(|text| !text.trim().is_empty())
        || existing_block_text.is_some_and(|text| {
            contains_case_insensitive(text, "<param")
                && contains_case_insensitive(text, &format!("name=\"{name}\""))
                && contains_case_insensitive(text, "</param>")
        })
}

fn documentation_symbol_type_name(symbol: &Value) -> String {
    symbol_type_name(symbol).unwrap_or("Variant").to_string()
}

fn parameter_symbol_for_callable<'a>(
    callable: &Value,
    parameter_name: &str,
    symbols: &'a [Value],
) -> Option<&'a Value> {
    let scope = callable.get("scopeRange")?;
    symbols.iter().find(|candidate| {
        candidate.get("kind").and_then(Value::as_str) == Some("parameter")
            && candidate
                .get("name")
                .and_then(Value::as_str)
                .is_some_and(|name| name.eq_ignore_ascii_case(parameter_name))
            && candidate.get("sourceUri") == callable.get("sourceUri")
            && candidate
                .get("scopeRange")
                .is_some_and(|candidate_scope| same_range(candidate_scope, scope))
    })
}

fn documentation_comment_block_before(text: &str, declaration_line: u32) -> Option<(u32, String)> {
    let lines = text.split_inclusive('\n').collect::<Vec<_>>();
    let mut line = declaration_line.checked_sub(1)?;
    let mut start_line = None;
    loop {
        let current = lines.get(usize::try_from(line).ok()?)?;
        if !current.trim_start().starts_with('\'') {
            break;
        }
        start_line = Some(line);
        if line == 0 {
            break;
        }
        line -= 1;
    }
    let start_line = start_line?;
    let start_offset = line_start_byte_offset(text, start_line)?;
    let end_offset = line_start_byte_offset(text, declaration_line)?;
    Some((start_line, text[start_offset..end_offset].to_string()))
}

fn line_start_byte_offset(text: &str, target_line: u32) -> Option<usize> {
    if target_line == 0 {
        return Some(0);
    }
    let mut line = 0u32;
    for (offset, character) in text.char_indices() {
        if character == '\n' {
            line += 1;
            if line == target_line {
                return Some(offset + 1);
            }
        }
    }
    (line == target_line).then_some(text.len())
}

fn contains_case_insensitive(text: &str, needle: &str) -> bool {
    text.to_ascii_lowercase()
        .contains(&needle.to_ascii_lowercase())
}

fn preferred_new_line(text: &str) -> &str {
    if text.contains("\r\n") {
        "\r\n"
    } else {
        "\n"
    }
}

fn line_indent(text: &str, target_line: u32) -> String {
    let mut line = 0;
    for current in text.split_inclusive('\n') {
        if line == target_line {
            let current = current.trim_end_matches(['\r', '\n']);
            return current
                .chars()
                .take_while(|character| matches!(character, ' ' | '\t'))
                .collect();
        }
        line += 1;
    }
    String::new()
}

fn visible_inlay_type_name(symbol: &Value) -> Option<&str> {
    let type_name = symbol.get("typeName").and_then(Value::as_str)?;
    (!type_name.eq_ignore_ascii_case("unknown")).then_some(type_name)
}

fn variable_type_hint_position(text: &str, symbol: &Value) -> Option<Value> {
    let range = symbol.get("range")?;
    if symbol.get("array").and_then(Value::as_bool) == Some(true) {
        let name_end = range_end_offset(text, range)?;
        if let Some(close_paren_end) = declaration_close_paren_end(text, name_end) {
            let (line, character) = utf16_position_at(text, close_paren_end)?;
            return Some(serde_json::json!({ "line": line, "character": character }));
        }
    }
    range.get("end").cloned()
}

fn function_return_hint_position(text: &str, symbol: &Value) -> Option<Value> {
    let range = symbol.get("range")?;
    let name_end = range_end_offset(text, range)?;
    if let Some(close_paren_end) = declaration_close_paren_end(text, name_end) {
        let (line, character) = utf16_position_at(text, close_paren_end)?;
        return Some(serde_json::json!({ "line": line, "character": character }));
    }
    range.get("end").cloned()
}

fn declaration_close_paren_end(text: &str, name_end: usize) -> Option<usize> {
    let chars = utf16_chars(text);
    let mut depth = 0usize;
    let mut seen_open = false;
    for (offset, character) in chars.into_iter().filter(|(offset, _)| *offset >= name_end) {
        if character == '\r' || character == '\n' {
            return None;
        }
        if character == '(' {
            depth += 1;
            seen_open = true;
        } else if character == ')' && seen_open {
            depth = depth.saturating_sub(1);
            if depth == 0 {
                return Some(offset + character.len_utf16());
            }
        }
    }
    None
}

fn scope_inlay_prefix(symbol: &Value, mode: &str) -> &'static str {
    if mode == "off" || !is_variable_marker_symbol(symbol) {
        return "";
    }
    if is_global_variable_like_symbol(symbol) {
        return if mode == "global" || mode == "all" {
            " (global)"
        } else {
            ""
        };
    }
    if is_local_variable_like_symbol(symbol) {
        return if mode == "local" || mode == "all" {
            " (local)"
        } else {
            ""
        };
    }
    ""
}

fn is_variable_marker_symbol(symbol: &Value) -> bool {
    matches!(
        symbol.get("kind").and_then(Value::as_str),
        Some("variable" | "constant")
    ) && symbol.get("memberOf").and_then(Value::as_str).is_none()
}

fn is_global_variable_like_symbol(symbol: &Value) -> bool {
    is_variable_marker_symbol(symbol) && symbol.get("scopeName").and_then(Value::as_str).is_none()
}

fn is_local_variable_like_symbol(symbol: &Value) -> bool {
    is_variable_marker_symbol(symbol) && symbol.get("scopeName").and_then(Value::as_str).is_some()
}

fn has_explicit_parameter_mode_before(text: &str, parameter_start: usize) -> bool {
    let chars = utf16_chars(text);
    let mut previous = Vec::new();
    for (offset, character) in chars.into_iter().rev() {
        if offset >= parameter_start {
            continue;
        }
        if matches!(character, '(' | ',' | '\r' | '\n') {
            break;
        }
        previous.push(character);
    }
    let previous = previous.into_iter().rev().collect::<String>();
    let previous = previous.trim_end().to_ascii_lowercase();
    previous.ends_with("byref") || previous.ends_with("byval")
}

fn range_size(range: &Value) -> Option<usize> {
    let start = range.get("start")?;
    let end = range.get("end")?;
    let start_line = start.get("line")?.as_u64()?;
    let start_character = start.get("character")?.as_u64()?;
    let end_line = end.get("line")?.as_u64()?;
    let end_character = end.get("character")?.as_u64()?;
    Some(
        usize::try_from(end_line.saturating_sub(start_line)).ok()? * 100_000
            + usize::try_from(end_character.saturating_sub(start_character)).ok()?,
    )
}

fn valid_vb_identifier(name: &str) -> bool {
    let mut chars = name.chars();
    chars
        .next()
        .is_some_and(|character| character.is_ascii_alphabetic())
        && chars.all(is_identifier_char)
}

fn is_callable_symbol(symbol: &Value) -> bool {
    matches!(
        symbol.get("kind").and_then(Value::as_str),
        Some("function" | "sub" | "method" | "property")
    )
}

fn is_type_hierarchy_symbol(symbol: &Value) -> bool {
    matches!(symbol.get("kind").and_then(Value::as_str), Some("class"))
}

fn hierarchy_item_from_symbol(uri: &str, symbol: &Value) -> Option<Value> {
    let display_name = hierarchy_symbol_name(symbol)?;
    let range = symbol
        .get("scopeRange")
        .or_else(|| symbol.get("range"))?
        .clone();
    let selection_range = symbol.get("range")?.clone();
    Some(serde_json::json!({
        "name": display_name,
        "kind": symbol_kind(symbol),
        "uri": symbol.get("sourceUri").and_then(Value::as_str).unwrap_or(uri),
        "range": range,
        "selectionRange": selection_range,
        "data": {
            "uri": symbol.get("sourceUri").and_then(Value::as_str).unwrap_or(uri),
            "name": display_name,
        },
    }))
}

fn call_hierarchy_target_symbol<'a>(item: &Value, symbols: &'a [Value]) -> Option<&'a Value> {
    let item_uri = hierarchy_item_uri(item)?;
    let item_name = hierarchy_item_name(item)?;
    let selection_range = item.get("selectionRange")?;
    symbols.iter().find(|symbol| {
        is_callable_symbol(symbol)
            && symbol
                .get("sourceUri")
                .and_then(Value::as_str)
                .is_some_and(|source_uri| source_uri == item_uri)
            && symbol
                .get("range")
                .is_some_and(|range| same_range(range, selection_range))
            && hierarchy_symbol_name(symbol)
                .is_some_and(|symbol_name| symbol_name.eq_ignore_ascii_case(item_name))
    })
}

fn hierarchy_symbol_name(symbol: &Value) -> Option<String> {
    let name = symbol.get("name")?.as_str()?;
    Some(
        symbol
            .get("memberOf")
            .and_then(Value::as_str)
            .map(|owner| format!("{owner}.{name}"))
            .unwrap_or_else(|| name.to_string()),
    )
}

fn hierarchy_item_name(item: &Value) -> Option<&str> {
    item.pointer("/data/name")
        .and_then(Value::as_str)
        .or_else(|| item.get("name").and_then(Value::as_str))
}

fn hierarchy_item_uri(item: &Value) -> Option<&str> {
    item.pointer("/data/uri")
        .and_then(Value::as_str)
        .or_else(|| item.get("uri").and_then(Value::as_str))
}

fn enclosing_callable_symbol<'a>(
    text: &str,
    symbols: &'a [Value],
    offset: usize,
) -> Option<&'a Value> {
    symbols
        .iter()
        .filter(|symbol| is_callable_symbol(symbol))
        .filter_map(|symbol| {
            let range = symbol.get("scopeRange").or_else(|| symbol.get("range"))?;
            let start = range_start_offset(text, range)?;
            let end = range_end_offset(text, range)?;
            (offset >= start && offset <= end).then_some((symbol, end.saturating_sub(start)))
        })
        .min_by_key(|(_, length)| *length)
        .map(|(symbol, _)| symbol)
}

fn merge_call_hierarchy_entry(
    calls: &mut Vec<Value>,
    item_key: &str,
    item: Value,
    from_range: Value,
) {
    let Some(name) = item.get("name").and_then(Value::as_str) else {
        return;
    };
    let Some(uri) = item.get("uri").and_then(Value::as_str) else {
        return;
    };
    if let Some(existing) = calls.iter_mut().find(|call| {
        call.get(item_key)
            .is_some_and(|candidate| candidate.get("name").and_then(Value::as_str) == Some(name))
            && call
                .get(item_key)
                .is_some_and(|candidate| candidate.get("uri").and_then(Value::as_str) == Some(uri))
    }) {
        if let Some(ranges) = existing.get_mut("fromRanges").and_then(Value::as_array_mut) {
            ranges.push(from_range);
        }
        return;
    }
    calls.push(serde_json::json!({
        item_key: item,
        "fromRanges": [from_range],
    }));
}

fn symbol_signature(symbol: &Value) -> Option<String> {
    let name = symbol.get("name")?.as_str()?;
    let kind = symbol
        .get("kind")
        .and_then(Value::as_str)
        .unwrap_or("symbol");
    let parameters = symbol
        .get("parameters")
        .and_then(Value::as_array)
        .into_iter()
        .flatten()
        .filter_map(Value::as_str)
        .collect::<Vec<_>>()
        .join(", ");
    let type_name = symbol.get("typeName").and_then(Value::as_str);
    let keyword = match kind {
        "function" => "Function",
        "sub" => "Sub",
        "property" => "Property",
        "class" => "Class",
        "variable" => "Dim",
        "constant" => "Const",
        _ => "",
    };
    Some(match (keyword, type_name) {
        ("Function", Some(type_name)) => format!("Function {name}({parameters}) As {type_name}"),
        ("Sub", _) => format!("Sub {name}({parameters})"),
        ("Property", Some(type_name)) => format!("Property {name}({parameters}) As {type_name}"),
        ("Class", _) => format!("Class {name}"),
        ("Dim", Some(type_name)) => format!("Dim {name} As {type_name}"),
        ("Const", Some(type_name)) => format!("Const {name} As {type_name}"),
        _ => name.to_string(),
    })
}

fn symbol_detail(symbol: &Value) -> Value {
    symbol_signature(symbol)
        .map(Value::String)
        .unwrap_or(Value::Null)
}

fn completion_kind(symbol: &Value) -> u32 {
    match symbol.get("kind").and_then(Value::as_str) {
        Some("function" | "sub" | "method") => 3,
        Some("property") => 10,
        Some("class") => 7,
        Some("constant") => 21,
        Some("parameter" | "variable") => 6,
        _ => 1,
    }
}

struct BuiltinCompletionDetail {
    detail: Value,
    documentation: String,
}

fn builtin_catalog() -> &'static Value {
    static CATALOG: OnceLock<Value> = OnceLock::new();
    CATALOG.get_or_init(|| {
        serde_json::from_str(VBSCRIPT_BUILTIN_CATALOG_JSON)
            .expect("shared VBScript builtin catalog must be valid JSON")
    })
}

fn builtin_completion_items(prefix: &str) -> Vec<Value> {
    let mut items = Vec::new();
    for label in [
        "Request",
        "Response",
        "Session",
        "Application",
        "Server",
        "ASPError",
    ] {
        if completion_prefix_matches(label, prefix) {
            items.push(serde_json::json!({
                "label": label,
                "kind": if label == "ASPError" { 7 } else { 6 },
                "data": { "kind": "vbscript-builtin" },
            }));
        }
    }
    for item in builtin_catalog()
        .get("functions")
        .and_then(Value::as_array)
        .into_iter()
        .flatten()
    {
        let Some(label) = item.get("label").and_then(Value::as_str) else {
            continue;
        };
        if completion_prefix_matches(label, prefix) {
            items.push(serde_json::json!({
                "label": label,
                "kind": 3,
                "data": { "kind": "vbscript-builtin" },
            }));
        }
    }
    for item in builtin_catalog()
        .get("constants")
        .and_then(Value::as_array)
        .into_iter()
        .flatten()
    {
        let Some(label) = item.get("label").and_then(Value::as_str) else {
            continue;
        };
        if completion_prefix_matches(label, prefix) {
            items.push(serde_json::json!({
                "label": label,
                "kind": 21,
                "data": { "kind": "vbscript-builtin" },
            }));
        }
    }
    items
}

fn builtin_member_completion_items(type_name: &str, prefix: &str) -> Vec<Value> {
    let Some(object) = builtin_object_spec(type_name) else {
        return Vec::new();
    };
    let owner = object
        .get("typeName")
        .and_then(Value::as_str)
        .unwrap_or(type_name);
    object
        .get("members")
        .and_then(Value::as_array)
        .into_iter()
        .flatten()
        .filter_map(|member| {
            let label = member.get("name").and_then(Value::as_str)?;
            if !completion_prefix_matches(label, prefix) {
                return None;
            }
            let kind = member
                .get("kind")
                .and_then(Value::as_str)
                .map(completion_kind_for_builtin_member)
                .unwrap_or(10);
            let detail = builtin_member_detail(member);
            Some(serde_json::json!({
                "label": label,
                "kind": kind,
                "detail": detail,
                "data": { "kind": "vbscript-builtin-member", "owner": owner },
            }))
        })
        .collect()
}

fn completion_prefix_matches(label: &str, prefix: &str) -> bool {
    prefix.is_empty() || label.to_lowercase().starts_with(&prefix.to_lowercase())
}

fn builtin_completion_detail(label: &str) -> Option<BuiltinCompletionDetail> {
    if let Some(function) = builtin_catalog()
        .get("functions")
        .and_then(Value::as_array)
        .into_iter()
        .flatten()
        .find(|item| {
            item.get("label")
                .and_then(Value::as_str)
                .is_some_and(|candidate| candidate.eq_ignore_ascii_case(label))
        })
    {
        let signature = function.get("signature")?.as_str()?;
        let return_type = function
            .get("returnType")
            .and_then(Value::as_str)
            .unwrap_or("Variant");
        let summary = function
            .get("summary")
            .and_then(Value::as_str)
            .unwrap_or("");
        let signature = format!("Function {signature} As {return_type}");
        return Some(BuiltinCompletionDetail {
            detail: Value::String(signature.clone()),
            documentation: builtin_markdown(&signature, summary),
        });
    }

    if let Some(constant) = builtin_catalog()
        .get("constants")
        .and_then(Value::as_array)
        .into_iter()
        .flatten()
        .find(|item| {
            item.get("label")
                .and_then(Value::as_str)
                .is_some_and(|candidate| candidate.eq_ignore_ascii_case(label))
        })
    {
        let label = constant.get("label")?.as_str()?;
        let type_name = constant
            .get("type")
            .and_then(Value::as_str)
            .unwrap_or("Variant");
        let summary = constant
            .get("summary")
            .and_then(Value::as_str)
            .unwrap_or("");
        let signature = format!("Const {label} As {type_name}");
        return Some(BuiltinCompletionDetail {
            detail: Value::String(signature.clone()),
            documentation: builtin_markdown(&signature, summary),
        });
    }

    let type_name = classic_asp_builtin_type_name(label)?;
    let signature = format!("{type_name} object");
    Some(BuiltinCompletionDetail {
        detail: Value::String(format!("Classic ASP {type_name} object")),
        documentation: builtin_markdown(&signature, "Built-in Classic ASP runtime object."),
    })
}

fn builtin_member_completion_detail(owner: &str, label: &str) -> Option<BuiltinCompletionDetail> {
    let member = builtin_member_spec(owner, label)?;
    let detail = builtin_member_detail(member);
    let signature = member
        .get("signature")
        .and_then(Value::as_str)
        .map(str::to_string)
        .unwrap_or_else(|| format!("{owner}.{label}"));
    let type_name = member
        .get("type")
        .and_then(Value::as_str)
        .unwrap_or("Variant");
    let summary = format!("Built-in Classic ASP {owner} member.");
    Some(BuiltinCompletionDetail {
        detail,
        documentation: builtin_markdown(&format!("{signature} As {type_name}"), &summary),
    })
}

fn builtin_member_detail(member: &Value) -> Value {
    let kind = member
        .get("kind")
        .and_then(Value::as_str)
        .unwrap_or("property");
    let Some(type_name) = member.get("type").and_then(Value::as_str) else {
        return Value::String(kind.to_string());
    };
    Value::String(format!("{kind} As {type_name}"))
}

fn builtin_member_spec(type_name: &str, label: &str) -> Option<&'static Value> {
    builtin_object_spec(type_name)?
        .get("members")
        .and_then(Value::as_array)?
        .iter()
        .find(|member| {
            member
                .get("name")
                .and_then(Value::as_str)
                .is_some_and(|candidate| candidate.eq_ignore_ascii_case(label))
        })
}

fn builtin_object_spec(type_name: &str) -> Option<&'static Value> {
    ["classicAspObjects", "externalObjects"]
        .into_iter()
        .find_map(|section| {
            builtin_catalog()
                .get(section)?
                .as_object()?
                .values()
                .find(|object| {
                    object
                        .get("typeName")
                        .and_then(Value::as_str)
                        .is_some_and(|candidate| candidate.eq_ignore_ascii_case(type_name))
                })
        })
}

fn completion_kind_for_builtin_member(kind: &str) -> u32 {
    match kind {
        "method" => 2,
        "property" => 10,
        "field" => 5,
        "event" => 23,
        _ => 10,
    }
}

fn classic_asp_builtin_type_name(label: &str) -> Option<&'static str> {
    match label.to_ascii_lowercase().as_str() {
        "request" => Some("Request"),
        "response" => Some("Response"),
        "session" => Some("Session"),
        "application" => Some("Application"),
        "server" => Some("Server"),
        "asperror" => Some("ASPError"),
        _ => None,
    }
}

fn builtin_markdown(signature: &str, summary: &str) -> String {
    if summary.is_empty() {
        format!("```vbscript\n{signature}\n```")
    } else {
        format!("```vbscript\n{signature}\n```\n\n{summary}")
    }
}

fn symbol_kind(symbol: &Value) -> u32 {
    match symbol.get("kind").and_then(Value::as_str) {
        Some("function") => 12,
        Some("sub" | "method") => 6,
        Some("property") => 7,
        Some("class") => 5,
        Some("constant") => 14,
        Some("variable" | "parameter") => 13,
        _ => 13,
    }
}

fn document_symbol_kind(symbol: &Value) -> u32 {
    match symbol.get("kind").and_then(Value::as_str) {
        Some("class") => 5,
        Some("property") => 7,
        _ => 12,
    }
}

fn call_name_before_offset(text: &str, offset: usize) -> Option<String> {
    let before = slice_utf16(text, 0, offset).ok()?;
    let open = before.rfind('(')?;
    let name_end = before[..open].trim_end().len();
    let name_start = before[..name_end]
        .rfind(|character: char| !is_identifier_char(character))
        .map(|index| index + 1)
        .unwrap_or(0);
    (name_start < name_end).then(|| before[name_start..name_end].to_string())
}

fn active_parameter(text: &str, offset: usize) -> usize {
    let Some(before) = slice_utf16(text, 0, offset).ok() else {
        return 0;
    };
    let Some(open) = before.rfind('(') else {
        return 0;
    };
    before[open + 1..]
        .chars()
        .filter(|character| *character == ',')
        .count()
}

fn folding_range_from_lsp_range(range: &Value) -> Option<Value> {
    let start_line = range.pointer("/start/line")?.as_u64()?;
    let end_line = range.pointer("/end/line")?.as_u64()?;
    (end_line > start_line).then(|| {
        serde_json::json!({
            "startLine": start_line,
            "endLine": end_line,
        })
    })
}

fn semantic_symbol_kind(symbol: &Value) -> Option<(u32, u32)> {
    let token_type = match symbol.get("kind").and_then(Value::as_str) {
        Some("class") => 4,
        Some("method") => 5,
        Some("field" | "property") => 6,
        Some("function" | "sub") => 3,
        Some("parameter") => 2,
        Some("constant" | "variable") => 1,
        _ => return None,
    };
    let mut modifiers = 0;
    match symbol.get("visibility").and_then(Value::as_str) {
        Some("public") => modifiers |= 1 << 0,
        Some("private") => modifiers |= 1 << 1,
        _ => {}
    }
    if symbol.get("kind").and_then(Value::as_str) == Some("constant") {
        modifiers |= 1 << 2;
    }
    if symbol.get("kind").and_then(Value::as_str) == Some("parameter") {
        match symbol.get("parameterMode").and_then(Value::as_str) {
            Some("byval") => modifiers |= 1 << 5,
            _ => modifiers |= 1 << 4,
        }
    }
    Some((token_type, modifiers))
}

fn builtin_semantic_token(
    text: &str,
    identifier: &IdentifierOccurrence,
    parsed: &Value,
) -> Option<(u32, u32)> {
    let name = identifier.name.to_lowercase();
    if matches!(name.as_str(), "cstr" | "ubound" | "array") {
        return Some((3, 1 << 3));
    }
    if matches!(
        name.as_str(),
        "response" | "request" | "server" | "session" | "application" | "asperror"
    ) {
        return Some((1, 1 << 3));
    }
    if name == "write" && previous_identifier_is(text, parsed, identifier.start, "response") {
        return Some((5, 1 << 3));
    }
    None
}

fn is_classic_asp_object_name(name: &str) -> bool {
    matches!(
        name.to_lowercase().as_str(),
        "request" | "response" | "session" | "application" | "server" | "asperror"
    )
}

fn previous_identifier_is(text: &str, parsed: &Value, offset: usize, expected: &str) -> bool {
    let Ok(before) = slice_utf16(text, 0, offset) else {
        return false;
    };
    let before = before.trim_end();
    let Some(without_dot) = before.strip_suffix('.') else {
        return false;
    };
    let owner_end = utf16_len(without_dot.trim_end());
    let Some(owner) = identifier_at_offset(text, owner_end) else {
        return false;
    };
    owner.eq_ignore_ascii_case(expected) && is_vbscript_offset(parsed, owner_end)
}

fn resolve_include_target(uri: &str, include: &Value) -> String {
    let path = include
        .get("path")
        .and_then(Value::as_str)
        .unwrap_or_default();
    if path.starts_with("file://") {
        return path.to_string();
    }
    let Some((base, _)) = uri.rsplit_once('/') else {
        return path.to_string();
    };
    format!("{base}/{path}")
}

fn line_range(text: &str, target_line: u32) -> Option<Value> {
    let mut line = 0;
    let mut character = 0;
    let mut start = None;
    let mut end = None;
    for current in text.chars() {
        if line == target_line && start.is_none() {
            start = Some((line, character));
        }
        if current == '\n' {
            if line == target_line {
                end = Some((line, character));
                break;
            }
            line += 1;
            character = 0;
        } else {
            character += current.len_utf16() as u32;
        }
    }
    if line == target_line && end.is_none() {
        end = Some((line, character));
    }
    let (start_line, start_character) = start?;
    let (end_line, end_character) = end?;
    Some(serde_json::json!({
        "start": { "line": start_line, "character": start_character },
        "end": { "line": end_line, "character": end_character },
    }))
}

fn next_non_whitespace_offset(text: &str, start: usize, expected: char) -> Option<usize> {
    let chars = utf16_chars(text);
    for (offset, character) in chars.into_iter().filter(|(offset, _)| *offset >= start) {
        if character.is_whitespace() {
            continue;
        }
        return (character == expected).then_some(offset);
    }
    None
}

fn include_semantic_tokens(_text: &str, parsed: &Value) -> Vec<SemanticToken> {
    let mut tokens = Vec::new();
    for include in parsed
        .get("includes")
        .and_then(Value::as_array)
        .into_iter()
        .flatten()
    {
        for (key, token_type) in [("directiveRange", 0), ("modeRange", 6), ("pathRange", 8)] {
            if let Some(range) = include.get(key).cloned() {
                tokens.push(SemanticToken {
                    range,
                    token_type,
                    token_modifiers: 0,
                });
            }
        }
    }
    tokens
}

fn asp_delimiter_semantic_tokens(text: &str, parsed: &Value) -> Vec<SemanticToken> {
    parsed
        .get("regions")
        .and_then(Value::as_array)
        .into_iter()
        .flatten()
        .filter(|region| region.get("kind").and_then(Value::as_str) == Some("asp-expression"))
        .filter_map(|region| {
            let start = value_usize(region, "start").ok()?.checked_add(2)?;
            let end = start.checked_add(1)?;
            Some(SemanticToken {
                range: range_from_offsets(text, start, end)?,
                token_type: 0,
                token_modifiers: 0,
            })
        })
        .collect()
}

fn operator_semantic_tokens(text: &str, parsed: &Value) -> Vec<SemanticToken> {
    let operators = ["&", "+", "-", "*", "/", "\\", "^", "=", "<", ">"];
    let mut tokens = Vec::new();
    let chars = utf16_chars(text);
    for (offset, character) in chars {
        if !operators.contains(&character.to_string().as_str())
            || !is_vbscript_offset(parsed, offset)
        {
            continue;
        }
        if let Some(range) = range_from_offsets(text, offset, offset + character.len_utf16()) {
            tokens.push(SemanticToken {
                range,
                token_type: 9,
                token_modifiers: 0,
            });
        }
    }
    tokens
}

fn encode_semantic_tokens(tokens: &[SemanticToken]) -> Vec<u64> {
    let mut data = Vec::new();
    let mut previous_line = 0;
    let mut previous_character = 0;
    for token in tokens {
        let Some(line) = token.range.pointer("/start/line").and_then(Value::as_u64) else {
            continue;
        };
        let Some(character) = token
            .range
            .pointer("/start/character")
            .and_then(Value::as_u64)
        else {
            continue;
        };
        let Some(end_character) = token
            .range
            .pointer("/end/character")
            .and_then(Value::as_u64)
        else {
            continue;
        };
        let delta_line = line.saturating_sub(previous_line);
        let delta_start = if delta_line == 0 {
            character.saturating_sub(previous_character)
        } else {
            character
        };
        data.extend([
            delta_line,
            delta_start,
            end_character.saturating_sub(character),
            u64::from(token.token_type),
            u64::from(token.token_modifiers),
        ]);
        previous_line = line;
        previous_character = character;
    }
    data
}

fn range_start_offset(text: &str, range: &Value) -> Option<usize> {
    position_to_utf16_offset(
        text,
        range.pointer("/start/line")?.as_u64()?.try_into().ok()?,
        range
            .pointer("/start/character")?
            .as_u64()?
            .try_into()
            .ok()?,
    )
}

fn range_end_offset(text: &str, range: &Value) -> Option<usize> {
    position_to_utf16_offset(
        text,
        range.pointer("/end/line")?.as_u64()?.try_into().ok()?,
        range.pointer("/end/character")?.as_u64()?.try_into().ok()?,
    )
}

#[derive(Clone)]
struct FormattingRegion {
    kind: String,
    language: String,
    start: usize,
    end: usize,
    content_start: usize,
    content_end: usize,
}

struct FormattingOptions {
    tab_size: usize,
    insert_spaces: bool,
    indent_size: Option<usize>,
    indent_style: Option<String>,
    ignore_vbscript_tag_indent: bool,
    uppercase_keywords: bool,
    align_assignments: bool,
}

impl FormattingOptions {
    fn from_values(lsp_options: &Value, settings: &Value) -> Self {
        let format = settings.get("format").unwrap_or(&Value::Null);
        Self {
            tab_size: lsp_options
                .get("tabSize")
                .and_then(Value::as_u64)
                .and_then(|value| value.try_into().ok())
                .unwrap_or(2),
            insert_spaces: lsp_options
                .get("insertSpaces")
                .and_then(Value::as_bool)
                .unwrap_or(true),
            indent_size: format
                .get("indentSize")
                .and_then(Value::as_u64)
                .and_then(|value| value.try_into().ok()),
            indent_style: format
                .get("indentStyle")
                .and_then(Value::as_str)
                .map(str::to_string),
            ignore_vbscript_tag_indent: format
                .get("ignoreVbscriptTagIndent")
                .and_then(Value::as_bool)
                .unwrap_or(false),
            uppercase_keywords: format
                .get("uppercaseKeywords")
                .and_then(Value::as_bool)
                .unwrap_or(false),
            align_assignments: format
                .get("alignAssignments")
                .and_then(Value::as_bool)
                .unwrap_or(false),
        }
    }

    fn indent_unit(&self) -> String {
        if self.indent_style.as_deref() == Some("tab")
            || (self.indent_style.is_none() && !self.insert_spaces)
        {
            "\t".to_string()
        } else {
            " ".repeat(self.indent_size.unwrap_or(self.tab_size))
        }
    }

    fn indent_size(&self) -> usize {
        self.indent_size.unwrap_or(self.tab_size)
    }
}

fn format_text(
    text: &str,
    parsed: &Value,
    options: &FormattingOptions,
    start: usize,
    end: usize,
) -> Result<String, String> {
    let mut regions = parsed
        .get("regions")
        .and_then(Value::as_array)
        .into_iter()
        .flatten()
        .filter_map(formatting_region)
        .filter(|region| region.end > start && region.start < end)
        .collect::<Vec<_>>();
    regions.sort_by_key(|region| {
        (
            region.start,
            if region.kind == "html" { 1 } else { 0 },
            std::cmp::Reverse(region.end),
        )
    });

    let mut pieces = Vec::new();
    let mut cursor = start;
    for region in regions {
        if region.end <= cursor {
            continue;
        }
        if region.start > cursor {
            pieces.push(slice_utf16(text, cursor, region.start.min(end))?);
        }
        let region_start = region.start.max(start);
        let region_end = region.end.min(end);
        if region_start < region_end {
            pieces.push(format_region(
                text,
                &region,
                options,
                region_start,
                region_end,
            )?);
        }
        cursor = cursor.max(region_end);
    }
    if cursor < end {
        pieces.push(slice_utf16(text, cursor, end)?);
    }
    Ok(pieces.join(""))
}

fn formatting_region(value: &Value) -> Option<FormattingRegion> {
    Some(FormattingRegion {
        kind: value.get("kind")?.as_str()?.to_string(),
        language: value.get("language")?.as_str()?.to_string(),
        start: value.get("start")?.as_u64()?.try_into().ok()?,
        end: value.get("end")?.as_u64()?.try_into().ok()?,
        content_start: value.get("contentStart")?.as_u64()?.try_into().ok()?,
        content_end: value.get("contentEnd")?.as_u64()?.try_into().ok()?,
    })
}

fn format_region(
    text: &str,
    region: &FormattingRegion,
    options: &FormattingOptions,
    start: usize,
    end: usize,
) -> Result<String, String> {
    if region.language != "vbscript" && region.kind != "asp-directive" {
        return slice_utf16(text, start, end);
    }
    if start != region.start || end != region.end {
        return format_vbscript_block(&slice_utf16(text, start, end)?, options, 0);
    }
    if region.kind == "asp-expression" {
        let expression = format_vbscript_line(
            slice_utf16(text, region.content_start, region.content_end)?.trim(),
            options,
        );
        return Ok(format!("<%= {expression} %>"));
    }
    if region.kind == "asp-directive" {
        let directive = one_line(&slice_utf16(
            text,
            region.content_start,
            region.content_end,
        )?);
        let normalized = directive.strip_prefix('@').unwrap_or(&directive).trim();
        return Ok(format!("<%@ {normalized} %>"));
    }
    let content = slice_utf16(text, region.content_start, region.content_end)?;
    if !content.contains('\n') && !content.contains('\r') {
        return Ok(format!(
            "<% {} %>",
            format_vbscript_line(content.trim(), options)
        ));
    }
    let base_indent = vbscript_tag_indent_level(text, region, options);
    let formatted = format_vbscript_block(&content, options, base_indent)?;
    let unit = options.indent_unit();
    if region.kind == "asp-block" {
        return Ok(format!("<%\n{formatted}\n{}%>", unit.repeat(base_indent)));
    }
    let before = slice_utf16(text, region.start, region.content_start)?;
    let after = slice_utf16(text, region.content_end, region.end)?;
    Ok(format!(
        "{before}\n{formatted}\n{}{after}",
        unit.repeat(base_indent)
    ))
}

fn format_vbscript_block(
    text: &str,
    options: &FormattingOptions,
    base_indent_level: usize,
) -> Result<String, String> {
    let unit = options.indent_unit();
    let normalized = text
        .trim_start_matches(['\r', '\n', ' ', '\t'])
        .trim_end_matches(['\r', '\n', ' ', '\t']);
    let mut indent_level = base_indent_level;
    let mut formatted = Vec::new();
    let mut previous_significant: Option<String> = None;
    for line in normalized.split('\n') {
        let trimmed = line.trim();
        if trimmed.is_empty() {
            formatted.push(String::new());
            continue;
        }
        let continues_previous = previous_significant
            .as_deref()
            .is_some_and(is_line_continuation);
        if !continues_previous && dedents_before_line(trimmed) {
            indent_level = indent_level.saturating_sub(1).max(base_indent_level);
        }
        let line_indent = indent_level + usize::from(continues_previous);
        let formatted_line = format_vbscript_line(trimmed, options);
        formatted.push(format!("{}{}", unit.repeat(line_indent), formatted_line));
        if !continues_previous && indents_after_line(trimmed) {
            indent_level += 1;
        }
        previous_significant = Some(formatted_line);
    }
    if options.align_assignments {
        formatted = align_assignments(formatted);
    }
    Ok(formatted.join("\n"))
}

fn align_assignments(mut lines: Vec<String>) -> Vec<String> {
    let mut group_start: Option<usize> = None;
    let mut max_left = 0;
    for index in 0..=lines.len() {
        let left_len = lines
            .get(index)
            .and_then(|line| assignment_parts(line))
            .map(|(left, _)| left.len());
        let Some(left_len) = left_len else {
            flush_assignment_group(&mut lines, group_start.take(), index, max_left);
            max_left = 0;
            continue;
        };
        if group_start.is_none() {
            group_start = Some(index);
        }
        max_left = max_left.max(left_len);
    }
    lines
}

fn flush_assignment_group(
    lines: &mut [String],
    group_start: Option<usize>,
    exclusive_end: usize,
    max_left: usize,
) {
    let Some(group_start) = group_start else {
        return;
    };
    if exclusive_end.saturating_sub(group_start) < 2 {
        return;
    }
    for line in &mut lines[group_start..exclusive_end] {
        let Some((left, right)) = assignment_parts(line) else {
            continue;
        };
        *line = format!("{left:<max_left$} = {right}");
    }
}

fn assignment_parts(line: &str) -> Option<(&str, &str)> {
    let (left, right) = line.split_once(" = ")?;
    if right.is_empty() || !is_assignment_left(left) {
        return None;
    }
    Some((left, right))
}

fn is_assignment_left(left: &str) -> bool {
    let trimmed = left.trim_start();
    let target = strip_case_insensitive_prefix(trimmed, "Set ")
        .map(str::trim_start)
        .unwrap_or(trimmed);
    is_assignment_target(target)
}

fn strip_case_insensitive_prefix<'a>(text: &'a str, prefix: &str) -> Option<&'a str> {
    let (candidate, rest) = text.split_at_checked(prefix.len())?;
    candidate.eq_ignore_ascii_case(prefix).then_some(rest)
}

fn is_assignment_target(target: &str) -> bool {
    let mut chars = target.chars();
    chars
        .next()
        .is_some_and(|character| character.is_ascii_alphabetic() || character == '_')
        && chars.all(|character| {
            character.is_ascii_alphanumeric() || character == '_' || character == '.'
        })
}

fn format_vbscript_line(line: &str, options: &FormattingOptions) -> String {
    let (code, comment) = split_vbscript_comment(line);
    let mut formatted = normalize_vbscript_code_spacing(code.trim(), options);
    if let Some(comment) = comment {
        if !formatted.is_empty() {
            formatted.push(' ');
        }
        formatted.push_str(comment.trim_start());
    }
    formatted
}

fn split_vbscript_comment(line: &str) -> (&str, Option<&str>) {
    let mut in_string = false;
    let mut previous_was_quote = false;
    for (index, character) in line.char_indices() {
        if character == '"' {
            if in_string && previous_was_quote {
                previous_was_quote = false;
                continue;
            }
            in_string = !in_string;
            previous_was_quote = true;
            continue;
        }
        previous_was_quote = false;
        if character == '\'' && !in_string {
            return (&line[..index], Some(&line[index..]));
        }
    }
    (line, None)
}

fn normalize_vbscript_code_spacing(code: &str, options: &FormattingOptions) -> String {
    let mut result = String::new();
    let mut chars = code.chars().peekable();
    let mut in_string = false;
    let mut pending_space = false;
    while let Some(character) = chars.next() {
        if character == '"' {
            result.push(character);
            if in_string && chars.peek() == Some(&'"') {
                result.push(chars.next().unwrap_or('"'));
            } else {
                in_string = !in_string;
            }
            continue;
        }
        if in_string {
            result.push(character);
            continue;
        }
        if character.is_whitespace() {
            pending_space = true;
            continue;
        }
        match character {
            '=' | ':' => {
                trim_trailing_space(&mut result);
                push_space_if_needed(&mut result);
                result.push(character);
                result.push(' ');
                pending_space = false;
            }
            ',' => {
                trim_trailing_space(&mut result);
                result.push(',');
                result.push(' ');
                pending_space = false;
            }
            ')' | ']' => {
                trim_trailing_space(&mut result);
                result.push(character);
                pending_space = false;
            }
            '.' | '(' | '[' => {
                trim_trailing_space(&mut result);
                result.push(character);
                pending_space = false;
            }
            _ => {
                if pending_space {
                    push_space_if_needed(&mut result);
                    pending_space = false;
                }
                if options.uppercase_keywords {
                    result.push_str(&uppercase_keyword(character, &mut chars));
                } else {
                    result.push(character);
                }
            }
        }
    }
    result.trim_end().to_string()
}

fn uppercase_keyword(first: char, chars: &mut std::iter::Peekable<std::str::Chars<'_>>) -> String {
    let mut word = first.to_string();
    while let Some(next) = chars.peek().copied() {
        if !is_identifier_char(next) {
            break;
        }
        word.push(chars.next().unwrap_or(next));
    }
    if is_vbscript_keyword(&word) {
        word.to_uppercase()
    } else {
        word
    }
}

fn push_space_if_needed(result: &mut String) {
    if !result.is_empty() && !result.ends_with(' ') {
        result.push(' ');
    }
}

fn trim_trailing_space(result: &mut String) {
    while result.ends_with(' ') {
        result.pop();
    }
}

fn dedents_before_line(line: &str) -> bool {
    let lower = line.to_lowercase();
    lower.starts_with("end ")
        || lower == "else"
        || lower.starts_with("elseif ")
        || lower.starts_with("next")
        || lower.starts_with("loop")
        || lower.starts_with("wend")
}

fn indents_after_line(line: &str) -> bool {
    let lower = line.to_lowercase();
    (lower.starts_with("class ")
        || lower.starts_with("sub ")
        || lower.starts_with("function ")
        || lower.starts_with("property ")
        || lower.starts_with("with ")
        || lower.starts_with("for ")
        || lower.starts_with("do")
        || lower.starts_with("while ")
        || lower.ends_with(" then"))
        && !lower.starts_with("end ")
}

fn is_line_continuation(line: &str) -> bool {
    line.trim_end().ends_with('_')
}

fn one_line(text: &str) -> String {
    text.split_whitespace().collect::<Vec<_>>().join(" ")
}

fn vbscript_tag_indent_level(
    text: &str,
    region: &FormattingRegion,
    options: &FormattingOptions,
) -> usize {
    if options.ignore_vbscript_tag_indent {
        return 0;
    }
    let line_start = line_start_offset(text, region.start);
    let indent = slice_utf16(text, line_start, region.start).unwrap_or_default();
    indent_width(&indent, options) / options.indent_size().max(1)
}

fn indent_width(indent: &str, options: &FormattingOptions) -> usize {
    indent
        .chars()
        .map(|character| {
            if character == '\t' {
                options.tab_size
            } else {
                1
            }
        })
        .sum()
}

fn line_start_offset(text: &str, offset: usize) -> usize {
    let mut start = 0;
    for (char_offset, character) in utf16_chars(text) {
        if char_offset >= offset {
            break;
        }
        if character == '\n' {
            start = char_offset + 1;
        }
    }
    start
}

fn line_end_offset(text: &str, offset: usize) -> usize {
    for (char_offset, character) in utf16_chars(text) {
        if char_offset >= offset && character == '\n' {
            return char_offset;
        }
    }
    utf16_len(text)
}

fn is_vbscript_keyword(word: &str) -> bool {
    matches!(
        word.to_lowercase().as_str(),
        "class"
            | "dim"
            | "do"
            | "else"
            | "elseif"
            | "end"
            | "for"
            | "function"
            | "if"
            | "loop"
            | "next"
            | "property"
            | "set"
            | "sub"
            | "then"
            | "wend"
            | "while"
            | "with"
    )
}

#[derive(Clone)]
struct EmbeddedRegion {
    language: String,
    kind: String,
    start: usize,
    end: usize,
    content_start: usize,
    content_end: usize,
}

fn build_virtual_documents(
    uri: String,
    source_text: &str,
    parsed: &Value,
) -> Result<Vec<MappedVirtualDocument>, String> {
    let mut regions = parsed
        .get("regions")
        .and_then(Value::as_array)
        .into_iter()
        .flatten()
        .map(embedded_region)
        .collect::<Result<Vec<_>, _>>()?;
    regions.sort_by_key(|region| (region.start, region.end));
    let mut documents = Vec::new();
    for language in ["html", "css", "javascript", "jscript"] {
        let language_regions = regions
            .iter()
            .filter(|region| region.language == language)
            .cloned()
            .collect::<Vec<_>>();
        if language != "html" && language_regions.is_empty() {
            continue;
        }
        documents.push(if language == "html" {
            build_masked_virtual_document(&uri, source_text, language, &language_regions)?
        } else {
            build_concatenated_virtual_document(
                &uri,
                source_text,
                language,
                &language_regions,
                &regions,
            )?
        });
    }
    Ok(documents)
}

fn embedded_region(value: &Value) -> Result<EmbeddedRegion, String> {
    Ok(EmbeddedRegion {
        language: value
            .get("language")
            .and_then(Value::as_str)
            .unwrap_or_default()
            .to_string(),
        kind: value
            .get("kind")
            .and_then(Value::as_str)
            .unwrap_or_default()
            .to_string(),
        start: value_usize(value, "start")?,
        end: value_usize(value, "end")?,
        content_start: value_usize(value, "contentStart")?,
        content_end: value_usize(value, "contentEnd")?,
    })
}

fn value_usize(value: &Value, key: &str) -> Result<usize, String> {
    value
        .get(key)
        .and_then(Value::as_u64)
        .and_then(|value| value.try_into().ok())
        .ok_or_else(|| format!("region.{key} must be a non-negative integer"))
}

fn build_masked_virtual_document(
    uri: &str,
    source_text: &str,
    language: &str,
    regions: &[EmbeddedRegion],
) -> Result<MappedVirtualDocument, String> {
    let mut chunks = Vec::new();
    let mut segments = Vec::new();
    let mut cursor = 0;
    for region in regions {
        if cursor < region.content_start {
            chunks.push(mask_utf16_range(
                source_text,
                cursor,
                region.content_start,
                " ",
            )?);
        }
        let content = slice_utf16(source_text, region.content_start, region.content_end)?;
        chunks.push(content);
        segments.push(SourceMapSegment {
            virtual_start: region.content_start,
            virtual_end: region.content_end,
            source_start: region.content_start,
            source_end: region.content_end,
        });
        cursor = region.content_end;
    }
    let source_len = utf16_len(source_text);
    if cursor < source_len {
        chunks.push(mask_utf16_range(source_text, cursor, source_len, " ")?);
    }
    let text = chunks.join("");
    Ok(mapped_virtual_document(
        uri,
        language,
        text,
        segments,
        source_text,
    ))
}

fn build_concatenated_virtual_document(
    uri: &str,
    source_text: &str,
    language: &str,
    regions: &[EmbeddedRegion],
    all_regions: &[EmbeddedRegion],
) -> Result<MappedVirtualDocument, String> {
    let mut text = String::new();
    let mut segments = Vec::new();
    for region in regions {
        let prefix = if language == "css" {
            if region.kind == "style-attribute" {
                "__asp_lsp__{"
            } else {
                "\n"
            }
        } else {
            ""
        };
        let suffix = if language == "css" && region.kind == "style-attribute" {
            "}\n"
        } else {
            "\n"
        };
        text.push_str(prefix);
        let virtual_start = utf16_len(&text);
        let content = masked_region_content(source_text, region, all_regions, language)?;
        text.push_str(&content);
        let virtual_end = utf16_len(&text);
        text.push_str(suffix);
        segments.extend(source_map_segments_for_region(
            region,
            all_regions,
            virtual_start,
        ));
        debug_assert_eq!(
            virtual_end - virtual_start,
            region.content_end - region.content_start
        );
    }
    Ok(mapped_virtual_document(
        uri,
        language,
        text,
        segments,
        source_text,
    ))
}

fn source_map_segments_for_region(
    owner: &EmbeddedRegion,
    all_regions: &[EmbeddedRegion],
    virtual_start: usize,
) -> Vec<SourceMapSegment> {
    let mut segments = Vec::new();
    let mut cursor = owner.content_start;
    for hole in all_regions.iter().filter(|region| {
        is_asp_hole(region)
            && region.start >= owner.content_start
            && region.end <= owner.content_end
            && region.start != owner.start
    }) {
        if cursor < hole.start {
            segments.push(source_map_segment(owner, virtual_start, cursor, hole.start));
        }
        cursor = cursor.max(hole.end);
    }
    if cursor < owner.content_end {
        segments.push(source_map_segment(
            owner,
            virtual_start,
            cursor,
            owner.content_end,
        ));
    }
    segments
}

fn source_map_segment(
    owner: &EmbeddedRegion,
    virtual_start: usize,
    source_start: usize,
    source_end: usize,
) -> SourceMapSegment {
    let offset = source_start - owner.content_start;
    SourceMapSegment {
        virtual_start: virtual_start + offset,
        virtual_end: virtual_start + offset + (source_end - source_start),
        source_start,
        source_end,
    }
}

fn masked_region_content(
    source_text: &str,
    owner: &EmbeddedRegion,
    all_regions: &[EmbeddedRegion],
    language: &str,
) -> Result<String, String> {
    let mut chunks = Vec::new();
    let mut cursor = owner.content_start;
    for nested in all_regions.iter().filter(|nested| {
        nested.start >= owner.content_start
            && nested.end <= owner.content_end
            && nested.start != owner.start
    }) {
        if nested.end <= cursor || nested.language == language {
            continue;
        }
        if cursor < nested.start {
            chunks.push(slice_utf16(source_text, cursor, nested.start)?);
        }
        chunks.push(nested_region_mask(source_text, owner, nested, language)?);
        cursor = nested.end;
    }
    if cursor < owner.content_end {
        chunks.push(slice_utf16(source_text, cursor, owner.content_end)?);
    }
    Ok(chunks.join(""))
}

fn nested_region_mask(
    source_text: &str,
    owner: &EmbeddedRegion,
    nested: &EmbeddedRegion,
    language: &str,
) -> Result<String, String> {
    if !is_asp_hole(nested) {
        return mask_utf16_range(source_text, nested.start, nested.end, " ");
    }
    if language == "css" {
        return mask_utf16_range(source_text, nested.start, nested.end, "x");
    }
    if language == "javascript" || language == "jscript" {
        let previous = previous_significant_char(source_text, owner.content_start, nested.start)?;
        if nested.kind == "asp-block"
            && !matches!(
                previous,
                Some(
                    '=' | '('
                        | '['
                        | ','
                        | ':'
                        | '?'
                        | '!'
                        | '~'
                        | '+'
                        | '-'
                        | '*'
                        | '/'
                        | '%'
                        | '&'
                        | '|'
                        | '^'
                        | '<'
                        | '>'
                )
            )
        {
            return mask_utf16_range(source_text, nested.start, nested.end, " ");
        }
        return first_value_placeholder_range(source_text, nested.start, nested.end, "0");
    }
    mask_utf16_range(source_text, nested.start, nested.end, " ")
}

fn is_asp_hole(region: &EmbeddedRegion) -> bool {
    matches!(
        region.kind.as_str(),
        "asp-block" | "asp-expression" | "asp-directive"
    )
}

fn previous_significant_char(text: &str, start: usize, end: usize) -> Result<Option<char>, String> {
    Ok(slice_utf16(text, start, end)?
        .chars()
        .rev()
        .find(|character| !character.is_whitespace()))
}

fn first_value_placeholder_range(
    text: &str,
    start: usize,
    end: usize,
    value: &str,
) -> Result<String, String> {
    let content = slice_utf16(text, start, end)?;
    let mut placed = false;
    Ok(content
        .chars()
        .map(|character| {
            if character == '\r' || character == '\n' {
                character.to_string()
            } else if !placed {
                placed = true;
                value.to_string()
            } else {
                " ".repeat(character.len_utf16())
            }
        })
        .collect())
}

fn mapped_virtual_document(
    uri: &str,
    language: &str,
    text: String,
    source_map: Vec<SourceMapSegment>,
    source_text: &str,
) -> MappedVirtualDocument {
    MappedVirtualDocument {
        document: VirtualDocument {
            uri: format!("{uri}.{language}.virtual"),
            language_id: language.to_string(),
            text,
        },
        source_map,
        source_text: source_text.to_string(),
    }
}

fn slice_utf16(text: &str, start: usize, end: usize) -> Result<String, String> {
    let start_byte = utf16_to_byte_offset(text, start)?;
    let end_byte = utf16_to_byte_offset(text, end)?;
    Ok(text[start_byte..end_byte].to_string())
}

fn mask_utf16_range(text: &str, start: usize, end: usize, fill: &str) -> Result<String, String> {
    let content = slice_utf16(text, start, end)?;
    Ok(content
        .chars()
        .map(|character| match character {
            '\r' | '\n' => character.to_string(),
            _ => fill.repeat(character.len_utf16()),
        })
        .collect())
}

fn utf16_len(text: &str) -> usize {
    text.encode_utf16().count()
}

fn utf16_to_byte_offset(text: &str, target_units: usize) -> Result<usize, String> {
    let mut units = 0;
    for (byte_index, character) in text.char_indices() {
        if units == target_units {
            return Ok(byte_index);
        }
        units += character.len_utf16();
        if units > target_units {
            return Err("offset splits a UTF-16 surrogate pair".to_string());
        }
    }
    if units == target_units {
        Ok(text.len())
    } else {
        Err(format!("offset {target_units} is out of bounds"))
    }
}

fn position_to_utf16_offset(
    text: &str,
    target_line: usize,
    target_character: usize,
) -> Option<usize> {
    let mut line = 0;
    let mut character = 0;
    let mut offset = 0;
    for current in text.chars() {
        if line == target_line && character == target_character {
            return Some(offset);
        }
        if current == '\n' {
            line += 1;
            character = 0;
        } else {
            character += current.len_utf16();
        }
        offset += current.len_utf16();
    }
    (line == target_line && character == target_character).then_some(offset)
}

fn utf16_position_at(text: &str, target_offset: usize) -> Option<(usize, usize)> {
    let mut line = 0;
    let mut character = 0;
    let mut offset = 0;
    for current in text.chars() {
        if offset == target_offset {
            return Some((line, character));
        }
        offset += current.len_utf16();
        if offset > target_offset {
            return None;
        }
        if current == '\n' {
            line += 1;
            character = 0;
        } else {
            character += current.len_utf16();
        }
    }
    (offset == target_offset).then_some((line, character))
}

struct WorkspaceSettingsState {
    input: WorkspaceSettings,
}

impl WorkspaceSettingsState {
    fn new(db: &IdeDatabase) -> Self {
        let input = WorkspaceSettings::new(db, "{}".to_string());
        Self { input }
    }

    fn set(&mut self, db: &mut IdeDatabase, settings: Value) -> Result<(), String> {
        let serialized = serde_json::to_string(&settings).map_err(|error| error.to_string())?;
        self.input.set_json(db).to(serialized);
        Ok(())
    }
}

fn utf16_character_to_char_offset(line_text: &str, target_units: usize) -> Result<usize, String> {
    let mut units = 0;
    for (char_index, character) in line_text.chars().enumerate() {
        if units == target_units {
            return Ok(char_index);
        }
        units += character.len_utf16();
        if units > target_units {
            return Err("position splits a UTF-16 surrogate pair".to_string());
        }
    }
    if units == target_units {
        Ok(line_text.chars().count())
    } else {
        Err(format!("character {target_units} is out of bounds"))
    }
}

fn resolve_include_uri(from_uri: &str, include: &Value) -> Option<String> {
    let path = include.get("path").and_then(Value::as_str)?;
    let mode = include
        .get("mode")
        .and_then(Value::as_str)
        .unwrap_or("file");
    if !from_uri.starts_with("file://") {
        return None;
    }
    let base = from_uri.strip_prefix("file://")?;
    let resolved = if mode == "virtual" || path.starts_with('/') {
        path.to_string()
    } else {
        let Some((directory, _)) = base.rsplit_once('/') else {
            return None;
        };
        format!("{directory}/{path}")
    };
    Some(format!("file://{}", normalize_path_segments(&resolved)))
}

fn normalize_path_segments(path: &str) -> String {
    let absolute = path.starts_with('/');
    let mut segments = Vec::new();
    for segment in path.split('/') {
        match segment {
            "" | "." => {}
            ".." => {
                segments.pop();
            }
            segment => segments.push(segment),
        }
    }
    let normalized = segments.join("/");
    if absolute {
        format!("/{normalized}")
    } else {
        normalized
    }
}

#[cfg(test)]
mod tests {
    use super::{Ide, TextPosition, TextRange};

    #[test]
    fn publishes_vbscript_diagnostics_for_open_document() {
        let mut ide = Ide::default();
        let diagnostics = ide
            .open_document(
                "file:///default.asp".to_string(),
                "<%\nOption Explicit\nmissingName = 1\n%>".to_string(),
            )
            .expect("diagnostics");

        assert!(diagnostics.iter().any(|diagnostic| {
            diagnostic
                .get("message")
                .and_then(serde_json::Value::as_str)
                .is_some_and(|message| message.contains("missingName"))
        }));
    }

    #[test]
    fn exposes_step_one_query_boundaries() {
        let mut ide = Ide::default();
        ide.set_open_document(
            "file:///site/default.asp".to_string(),
            "<!--#include file=\"shared.inc\"-->\n<%\nSub BuildTitle()\nEnd Sub\n%>".to_string(),
        );
        ide.set_open_document(
            "file:///site/shared.inc".to_string(),
            "<!--#include file=\"nested.inc\"-->\n<%\nDim SharedValue\n%>".to_string(),
        );
        ide.set_open_document(
            "file:///site/nested.inc".to_string(),
            "<%\nDim NestedValue\n%>".to_string(),
        );

        let parsed = ide
            .parse_asp("file:///site/default.asp")
            .expect("parse asp");
        assert_eq!(parsed["uri"], "file:///site/default.asp");

        let includes = ide
            .include_closure("file:///site/default.asp")
            .expect("include closure");
        assert!(includes.iter().any(|include| {
            include
                .get("path")
                .and_then(serde_json::Value::as_str)
                .is_some_and(|path| path == "shared.inc")
        }));
        assert!(includes.iter().any(|include| {
            include
                .get("path")
                .and_then(serde_json::Value::as_str)
                .is_some_and(|path| path == "nested.inc")
        }));

        let symbols = ide.vb_symbols("file:///site/default.asp").expect("symbols");
        assert!(symbols.iter().any(|symbol| {
            symbol
                .get("name")
                .and_then(serde_json::Value::as_str)
                .is_some_and(|name| name == "BuildTitle")
        }));

        let diagnostics = ide
            .vb_diagnostics("file:///site/default.asp")
            .expect("vb diagnostics");
        assert_eq!(diagnostics, Vec::<serde_json::Value>::new());
    }

    #[test]
    fn applies_incremental_change_with_utf16_positions() {
        let mut ide = Ide::default();
        ide.open_document(
            "file:///default.asp".to_string(),
            "<%\nOption Explicit\nDim declaredName\nmissingName = \"🍰\"\n%>".to_string(),
        )
        .expect("initial diagnostics");

        let diagnostics = ide
            .change_document_incremental(
                "file:///default.asp".to_string(),
                TextRange {
                    start: TextPosition {
                        line: 3,
                        character: 0,
                    },
                    end: TextPosition {
                        line: 3,
                        character: 11,
                    },
                },
                "declaredName".to_string(),
            )
            .expect("updated diagnostics");

        assert!(!diagnostics.iter().any(|diagnostic| {
            diagnostic
                .get("message")
                .and_then(serde_json::Value::as_str)
                .is_some_and(|message| message.contains("missingName"))
        }));
    }
}
