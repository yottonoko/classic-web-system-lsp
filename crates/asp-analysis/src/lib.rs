use asp_syntax::FileKind;
use serde_json::{json, Value};

mod core_bridge;

pub use core_bridge::{handle_json, handle_value, CoreState};

#[derive(Default)]
pub struct Analyzer {
    core: CoreState,
}

impl Analyzer {
    pub fn parse_asp(&mut self, uri: &str, text: &str, settings: &Value) -> Result<Value, String> {
        Ok(self.core.handle_value(&json!({
            "operation": "parseAspDocument",
            "uri": uri,
            "text": text,
            "settings": settings,
        }))?)
    }

    pub fn parse_asp_skeleton(
        &mut self,
        uri: &str,
        text: &str,
        settings: &Value,
    ) -> Result<Value, String> {
        Ok(self.core.handle_value(&json!({
            "operation": "parseAspDocumentSkeleton",
            "uri": uri,
            "text": text,
            "settings": settings,
        }))?)
    }

    pub fn parser_diagnostics(
        &mut self,
        uri: &str,
        text: &str,
        settings: &Value,
    ) -> Result<Vec<Value>, String> {
        if !matches!(FileKind::from_uri(uri), FileKind::Asp | FileKind::Inc) {
            return Ok(Vec::new());
        }

        let parsed = self.parse_asp_skeleton(uri, text, settings)?;
        Ok(parsed
            .get("diagnostics")
            .and_then(Value::as_array)
            .into_iter()
            .flatten()
            .cloned()
            .collect())
    }

    pub fn include_refs(
        &mut self,
        uri: &str,
        text: &str,
        settings: &Value,
    ) -> Result<Vec<Value>, String> {
        if !matches!(FileKind::from_uri(uri), FileKind::Asp | FileKind::Inc) {
            return Ok(Vec::new());
        }

        let parsed = self.parse_asp_skeleton(uri, text, settings)?;
        Ok(parsed
            .get("includes")
            .and_then(Value::as_array)
            .into_iter()
            .flatten()
            .cloned()
            .collect())
    }

    pub fn vb_symbols(
        &mut self,
        uri: &str,
        text: &str,
        settings: &Value,
    ) -> Result<Vec<Value>, String> {
        let context = settings
            .get("vbscript")
            .cloned()
            .unwrap_or_else(|| json!({}));
        let symbols = self.core.handle_value(&json!({
            "operation": "collectVbscriptSymbolsFromText",
            "uri": uri,
            "text": text,
            "settings": settings,
            "context": context,
        }))?;
        Ok(symbols.as_array().cloned().unwrap_or_default())
    }

    pub fn vb_diagnostics(
        &mut self,
        uri: &str,
        text: &str,
        settings: &Value,
    ) -> Result<Vec<Value>, String> {
        let semantic = self.analyze_vbscript(uri, text, settings)?;
        Ok(semantic
            .get("diagnostics")
            .and_then(Value::as_array)
            .into_iter()
            .flatten()
            .cloned()
            .collect())
    }

    fn analyze_vbscript(
        &mut self,
        uri: &str,
        text: &str,
        settings: &Value,
    ) -> Result<Value, String> {
        Ok(self.core.handle_value(&json!({
            "operation": "analyzeVbscriptFromText",
            "uri": uri,
            "text": text,
            "settings": settings,
            "context": settings.get("vbscript").cloned().unwrap_or_else(|| json!({})),
        }))?)
    }

    /// Runs the current Rust ASP/VBScript analyzer for a single source file.
    ///
    /// This is intentionally a small compatibility bridge for the first Rust
    /// LSP checkpoint. The follow-up crate split can move the parser and
    /// analyzer bodies behind typed APIs while keeping this result shape stable
    /// for LSP.
    pub fn analyze_document(
        &mut self,
        uri: &str,
        text: &str,
        settings: &Value,
    ) -> Result<Vec<Value>, String> {
        let parser_diagnostics = self.parser_diagnostics(uri, text, settings)?;
        let semantic = self.analyze_vbscript(uri, text, settings)?;

        let mut diagnostics = Vec::new();
        diagnostics.extend(parser_diagnostics);
        diagnostics.extend(
            semantic
                .get("diagnostics")
                .and_then(Value::as_array)
                .into_iter()
                .flatten()
                .cloned(),
        );
        Ok(diagnostics)
    }
}

pub fn parser_diagnostics_once(
    uri: &str,
    text: &str,
    settings: &Value,
) -> Result<Vec<Value>, String> {
    Analyzer::default().parser_diagnostics(uri, text, settings)
}

pub fn parse_asp_skeleton_once(uri: &str, text: &str, settings: &Value) -> Result<Value, String> {
    Analyzer::default().parse_asp_skeleton(uri, text, settings)
}

pub fn parse_asp_once(uri: &str, text: &str, settings: &Value) -> Result<Value, String> {
    Analyzer::default().parse_asp(uri, text, settings)
}

pub fn include_refs_once(uri: &str, text: &str, settings: &Value) -> Result<Vec<Value>, String> {
    Analyzer::default().include_refs(uri, text, settings)
}

pub fn vb_symbols_once(uri: &str, text: &str, settings: &Value) -> Result<Vec<Value>, String> {
    Analyzer::default().vb_symbols(uri, text, settings)
}

pub fn vb_diagnostics_once(uri: &str, text: &str, settings: &Value) -> Result<Vec<Value>, String> {
    Analyzer::default().vb_diagnostics(uri, text, settings)
}

pub fn analyze_document_once(
    uri: &str,
    text: &str,
    settings: &Value,
) -> Result<Vec<Value>, String> {
    Analyzer::default().analyze_document(uri, text, settings)
}

pub fn backend_status() -> Value {
    json!({
        "backend": "rust",
        "engine": "asp-lsp-server",
        "core": "asp-analysis",
        "version": env!("CARGO_PKG_VERSION"),
        "sidecar": { "status": "not-started" },
    })
}
