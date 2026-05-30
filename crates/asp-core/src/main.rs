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
    let envelope: serde_json::Value = match serde_json::from_str(line) {
        Ok(value) => value,
        Err(error) => {
            return json!({ "id": null, "ok": false, "error": error.to_string() }).to_string()
        }
    };
    let id = envelope
        .get("id")
        .cloned()
        .unwrap_or(serde_json::Value::Null);
    let request = match envelope.get("request") {
        Some(value) => value,
        None => {
            return json!({ "id": id, "ok": false, "error": "request is required" }).to_string()
        }
    };
    match state.handle_serialized_value(request) {
        Ok(result) => {
            let id_json = serde_json::to_string(&id).unwrap_or_else(|_| "null".to_string());
            format!(r#"{{"id":{id_json},"ok":true,"result":{result}}}"#)
        }
        Err(error) => json!({ "id": id, "ok": false, "error": error }).to_string(),
    }
}
