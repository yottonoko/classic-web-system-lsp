use serde_json::json;
use std::io::{self, BufRead, Read, Write};

fn main() {
    if std::env::args().any(|arg| arg == "--jsonl") {
        if let Err(error) = run_jsonl() {
            eprintln!("{error}");
            std::process::exit(1);
        }
        return;
    }
    if std::env::args().any(|arg| arg == "--frames") {
        if let Err(error) = run_frames() {
            eprintln!("{error}");
            std::process::exit(1);
        }
        return;
    }

    let mut input = String::new();
    if let Err(error) = io::stdin().read_to_string(&mut input) {
        eprintln!("failed to read stdin: {error}");
        std::process::exit(1);
    }
    match asp_lsp_core::handle_json(&input) {
        Ok(output) => {
            println!("{output}");
        }
        Err(error) => {
            eprintln!("{error}");
            std::process::exit(1);
        }
    }
}

fn run_jsonl() -> Result<(), String> {
    let stdin = io::stdin();
    let mut stdout = io::BufWriter::new(io::stdout());
    let mut state = asp_lsp_core::CoreState::default();
    for line in stdin.lock().lines() {
        let line = line.map_err(|error| format!("failed to read stdin: {error}"))?;
        if line.trim().is_empty() {
            continue;
        }
        let response = handle_jsonl_request(&mut state, &line);
        writeln!(stdout, "{response}")
            .map_err(|error| format!("failed to write stdout: {error}"))?;
        stdout
            .flush()
            .map_err(|error| format!("failed to flush stdout: {error}"))?;
    }
    Ok(())
}

fn handle_jsonl_request(state: &mut asp_lsp_core::CoreState, line: &str) -> String {
    match parse_jsonl_request(line) {
        JsonlRequest::Parsed { id, request } => format_json_response(state, id, &request),
        JsonlRequest::Error { id, error } => {
            json!({ "id": id, "ok": false, "error": error }).to_string()
        }
    }
}

fn run_frames() -> Result<(), String> {
    let stdin = io::stdin();
    let mut stdout = io::BufWriter::new(io::stdout());
    let mut state = asp_lsp_core::CoreState::default();
    for line in stdin.lock().lines() {
        let line = line.map_err(|error| format!("failed to read stdin: {error}"))?;
        if line.trim().is_empty() {
            continue;
        }
        let body = handle_frame_request(&mut state, &line);
        let body_len =
            u32::try_from(body.len()).map_err(|_| "frame body exceeds u32".to_string())?;
        stdout
            .write_all(&body_len.to_le_bytes())
            .map_err(|error| format!("failed to write stdout: {error}"))?;
        stdout
            .write_all(&body)
            .map_err(|error| format!("failed to write stdout: {error}"))?;
        stdout
            .flush()
            .map_err(|error| format!("failed to flush stdout: {error}"))?;
    }
    Ok(())
}

fn handle_frame_request(state: &mut asp_lsp_core::CoreState, line: &str) -> Vec<u8> {
    match parse_jsonl_request(line) {
        JsonlRequest::Parsed { id, request } => {
            if request.get("operation").and_then(serde_json::Value::as_str)
                == Some("parseAspDocumentVbscript")
            {
                match id.as_u64().and_then(|value| u32::try_from(value).ok()) {
                    Some(frame_id) => {
                        match state.handle_vbscript_columnar_frame_body(frame_id, &request) {
                            Ok(body) => body,
                            Err(error) => json_frame_body(
                                &json!({ "id": id, "ok": false, "error": error }).to_string(),
                            ),
                        }
                    }
                    None => json_frame_body(
                        &json!({ "id": id, "ok": false, "error": "id must be a u32 for frames" })
                            .to_string(),
                    ),
                }
            } else {
                json_frame_body(&format_json_response(state, id, &request))
            }
        }
        JsonlRequest::Error { id, error } => {
            json_frame_body(&json!({ "id": id, "ok": false, "error": error }).to_string())
        }
    }
}

enum JsonlRequest {
    Parsed {
        id: serde_json::Value,
        request: serde_json::Value,
    },
    Error {
        id: serde_json::Value,
        error: String,
    },
}

fn parse_jsonl_request(line: &str) -> JsonlRequest {
    let envelope: serde_json::Value = match serde_json::from_str(line) {
        Ok(value) => value,
        Err(error) => {
            return JsonlRequest::Error {
                id: serde_json::Value::Null,
                error: error.to_string(),
            }
        }
    };
    let id = envelope
        .get("id")
        .cloned()
        .unwrap_or(serde_json::Value::Null);
    let request = match envelope.get("request") {
        Some(value) => value.clone(),
        None => {
            return JsonlRequest::Error {
                id,
                error: "request is required".to_string(),
            }
        }
    };
    JsonlRequest::Parsed { id, request }
}

fn format_json_response(
    state: &mut asp_lsp_core::CoreState,
    id: serde_json::Value,
    request: &serde_json::Value,
) -> String {
    match state.handle_serialized_value(request) {
        Ok(result) => {
            let id_json = serde_json::to_string(&id).unwrap_or_else(|_| "null".to_string());
            format!(r#"{{"id":{id_json},"ok":true,"result":{result}}}"#)
        }
        Err(error) => json!({ "id": id, "ok": false, "error": error }).to_string(),
    }
}

fn json_frame_body(response: &str) -> Vec<u8> {
    let mut body = Vec::with_capacity(response.len() + 1);
    body.push(0);
    body.extend_from_slice(response.as_bytes());
    body
}
