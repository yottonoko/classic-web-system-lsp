use std::collections::HashMap;

use asp_analysis::Analyzer;
use serde_json::Value;

#[derive(Default)]
pub struct Ide {
    analyzer: Analyzer,
    documents: HashMap<String, String>,
    settings: Value,
}

impl Ide {
    pub fn set_settings(&mut self, settings: Value) {
        self.settings = settings;
    }

    pub fn open_document(&mut self, uri: String, text: String) -> Result<Vec<Value>, String> {
        self.documents.insert(uri.clone(), text);
        self.diagnostics(&uri)
    }

    pub fn change_document(&mut self, uri: String, text: String) -> Result<Vec<Value>, String> {
        self.documents.insert(uri.clone(), text);
        self.diagnostics(&uri)
    }

    pub fn close_document(&mut self, uri: &str) {
        self.documents.remove(uri);
    }

    pub fn diagnostics(&mut self, uri: &str) -> Result<Vec<Value>, String> {
        let Some(text) = self.documents.get(uri) else {
            return Ok(Vec::new());
        };
        self.analyzer.analyze_document(uri, text, &self.settings)
    }

    pub fn backend_status(&self) -> Value {
        asp_analysis::backend_status()
    }
}

#[cfg(test)]
mod tests {
    use super::Ide;

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
}
