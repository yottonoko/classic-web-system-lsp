use serde::{Deserialize, Serialize};

/// Workspace-stable identifier for a source file.
///
/// The first Rust LSP checkpoint keeps this as a URI string. The salsa-backed
/// database can intern it later without changing higher-level request shapes.
#[derive(Clone, Debug, Eq, Hash, PartialEq, Deserialize, Serialize)]
pub struct FileId(pub String);

/// File kind used by the Rust LSP pipeline to decide which analyzers can run.
#[derive(Clone, Copy, Debug, Eq, Hash, PartialEq, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub enum FileKind {
    Asp,
    Inc,
    ClientJs,
    ClientTs,
    Other,
}

impl FileKind {
    pub fn from_uri(uri: &str) -> Self {
        let lower = uri.to_ascii_lowercase();
        if lower.ends_with(".asp") || lower.ends_with(".asa") {
            Self::Asp
        } else if lower.ends_with(".inc") {
            Self::Inc
        } else if matches!(
            lower.rsplit_once('.').map(|(_, extension)| extension),
            Some("js" | "jsx" | "mjs" | "cjs")
        ) {
            Self::ClientJs
        } else if matches!(
            lower.rsplit_once('.').map(|(_, extension)| extension),
            Some("ts" | "tsx" | "mts" | "cts")
        ) {
            Self::ClientTs
        } else {
            Self::Other
        }
    }
}
