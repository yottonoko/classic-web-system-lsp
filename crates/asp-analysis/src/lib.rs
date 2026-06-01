use asp_syntax::FileKind;
use serde_json::{json, Value};

#[derive(Default)]
pub struct Analyzer {
    core: asp_lsp_core::CoreState,
}

impl Analyzer {
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
        if !matches!(FileKind::from_uri(uri), FileKind::Asp | FileKind::Inc) {
            return Ok(Vec::new());
        }

        let parser_diagnostics = self.core.handle_value(&json!({
            "operation": "parseAspDocumentSkeleton",
            "uri": uri,
            "text": text,
            "settings": settings,
        }))?;
        let semantic = self.core.handle_value(&json!({
            "operation": "analyzeVbscriptFromText",
            "uri": uri,
            "text": text,
            "settings": settings,
            "context": settings.get("vbscript").cloned().unwrap_or_else(|| json!({})),
        }))?;

        let mut diagnostics = Vec::new();
        diagnostics.extend(
            parser_diagnostics
                .get("diagnostics")
                .and_then(Value::as_array)
                .into_iter()
                .flatten()
                .cloned(),
        );
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

pub fn backend_status() -> Value {
    json!({
        "backend": "native",
        "engine": "asp-lsp-server",
        "core": "asp-lsp-core",
        "version": env!("CARGO_PKG_VERSION"),
        "sidecar": { "status": "not-started" },
    })
}
