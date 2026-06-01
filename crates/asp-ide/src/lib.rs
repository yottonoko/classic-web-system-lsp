use std::collections::HashMap;

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
