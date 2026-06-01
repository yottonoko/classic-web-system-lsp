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
