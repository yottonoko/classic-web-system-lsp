use serde::{Deserialize, Serialize};
use serde_json::Value;

#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct VirtualDocument {
    pub uri: String,
    pub language_id: String,
    pub text: String,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct SourceMapSegment {
    pub virtual_start: usize,
    pub virtual_end: usize,
    pub source_start: usize,
    pub source_end: usize,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct EmbeddedRequest {
    pub id: u64,
    pub operation: String,
    pub active_virtual: VirtualDocument,
    pub open_virtuals: Vec<VirtualDocument>,
    pub settings: Value,
    pub workspace_roots: Vec<String>,
    pub project_generation: u64,
    pub params: Value,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct EmbeddedResponse {
    pub id: u64,
    pub ok: bool,
    pub result: Option<Value>,
    pub error: Option<String>,
}

#[cfg(test)]
mod tests {
    use super::{EmbeddedRequest, VirtualDocument};
    use serde_json::{json, Value};

    #[test]
    fn serializes_project_generation_as_camel_case() {
        let request = EmbeddedRequest {
            id: 7,
            operation: "diagnostics".to_string(),
            active_virtual: VirtualDocument {
                uri: "file:///default.asp.javascript.virtual".to_string(),
                language_id: "javascript".to_string(),
                text: "externalValue.toFixed();".to_string(),
            },
            open_virtuals: Vec::new(),
            settings: json!({ "checkJs": true }),
            workspace_roots: vec!["file:///workspace".to_string()],
            project_generation: 42,
            params: Value::Null,
        };

        let serialized = serde_json::to_value(request).expect("serialize request");
        assert_eq!(serialized["projectGeneration"], json!(42));
        assert!(serialized.get("project_generation").is_none());
    }
}
