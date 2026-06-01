use std::collections::HashMap;

use asp_sidecar_protocol::{SourceMapSegment, VirtualDocument};
use ropey::Rope;
use salsa::Setter;
use serde_json::Value;

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

    pub fn diagnostics(&self, uri: &str) -> Result<Vec<Value>, String> {
        let Some(document) = self.documents.get(uri) else {
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
        for keyword in [
            "Dim", "Function", "Sub", "If", "Then", "Else", "End If", "For", "Next",
        ] {
            if prefix.is_empty() || keyword.to_lowercase().starts_with(&prefix.to_lowercase()) {
                items.push(serde_json::json!({ "label": keyword, "kind": 14 }));
            }
        }
        Ok(Value::Array(items))
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
                matches!(
                    symbol.get("kind").and_then(Value::as_str),
                    Some("function" | "sub" | "class" | "property" | "variable" | "constant")
                )
            })
            .filter_map(|symbol| {
                let name = symbol.get("name")?.as_str()?;
                let range = symbol
                    .get("scopeRange")
                    .or_else(|| symbol.get("range"))?
                    .clone();
                let selection_range = symbol.get("range")?.clone();
                Some(serde_json::json!({
                    "name": name,
                    "kind": symbol_kind(&symbol),
                    "range": range,
                    "selectionRange": selection_range,
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
        let highlights = identifier_ranges(&context.text, name)
            .into_iter()
            .map(|range| serde_json::json!({ "range": range, "kind": 1 }))
            .collect::<Vec<_>>();
        Ok(Value::Array(highlights))
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
            symbols,
        }))
    }

    fn symbol_at_position(
        &self,
        uri: &str,
        position: TextPosition,
    ) -> Result<Option<(Value, VbContext)>, String> {
        let Some(context) = self.vb_context(uri, position)? else {
            return Ok(None);
        };
        let Some(word) = identifier_at_offset(&context.text, context.offset) else {
            return Ok(None);
        };
        let symbol = context
            .symbols
            .iter()
            .find(|symbol| symbol_name_eq(symbol, &word))
            .cloned();
        Ok(symbol.map(|symbol| (symbol, context)))
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
    symbols: Vec<Value>,
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
        chars[start..end]
            .iter()
            .map(|(_, character)| *character)
            .collect()
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

fn identifier_ranges(text: &str, name: &str) -> Vec<Value> {
    let lower_name = name.to_lowercase();
    let chars = utf16_chars(text);
    let mut ranges = Vec::new();
    let mut index = 0;
    while index < chars.len() {
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
        if text_name.to_lowercase() == lower_name {
            let start_offset = chars[start].0;
            let end_offset = chars
                .get(index)
                .map(|(offset, _)| *offset)
                .unwrap_or_else(|| utf16_len(text));
            if let (Some((start_line, start_character)), Some((end_line, end_character))) = (
                utf16_position_at(text, start_offset),
                utf16_position_at(text, end_offset),
            ) {
                ranges.push(serde_json::json!({
                    "start": { "line": start_line, "character": start_character },
                    "end": { "line": end_line, "character": end_character },
                }));
            }
        }
    }
    ranges
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

fn is_callable_symbol(symbol: &Value) -> bool {
    matches!(
        symbol.get("kind").and_then(Value::as_str),
        Some("function" | "sub" | "method" | "property")
    )
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
