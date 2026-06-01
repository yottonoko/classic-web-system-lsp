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
pub struct EmbeddedRequest {
    pub id: u64,
    pub operation: String,
    pub active_virtual: VirtualDocument,
    pub open_virtuals: Vec<VirtualDocument>,
    pub settings: Value,
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
