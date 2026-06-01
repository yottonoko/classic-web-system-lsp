use std::io::{BufRead, BufReader, Write};
use std::path::PathBuf;
use std::process::{Command, Stdio};

use serde_json::{json, Value};

#[test]
fn publishes_diagnostics_over_stdio_lsp() {
    let mut child = Command::new(env!("CARGO_BIN_EXE_asp-lsp-server"))
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .spawn()
        .expect("spawn server");

    let mut stdin = child.stdin.take().expect("server stdin");
    let stdout = child.stdout.take().expect("server stdout");
    let mut reader = BufReader::new(stdout);

    write_message(
        &mut stdin,
        &json!({
            "jsonrpc": "2.0",
            "id": 1,
            "method": "initialize",
            "params": {
                "processId": std::process::id(),
                "rootUri": "file:///tmp",
                "capabilities": {},
            },
        }),
    );
    let initialize = read_message(&mut reader);
    assert_eq!(initialize["id"], json!(1));
    assert_eq!(
        initialize["result"]["serverInfo"]["name"],
        json!("asp-lsp-server")
    );

    write_message(
        &mut stdin,
        &json!({
            "jsonrpc": "2.0",
            "method": "initialized",
            "params": {},
        }),
    );
    write_message(
        &mut stdin,
        &json!({
            "jsonrpc": "2.0",
            "method": "textDocument/didOpen",
            "params": {
                "textDocument": {
                    "uri": "file:///tmp/default.asp",
                    "languageId": "classic-asp",
                    "version": 1,
                    "text": "<%\nOption Explicit\nmissingName = 1\n%>",
                },
            },
        }),
    );

    let diagnostics = read_until(&mut reader, |message| {
        message["method"] == json!("textDocument/publishDiagnostics")
            && message.to_string().contains("missingName")
    });
    assert_eq!(
        diagnostics["params"]["uri"],
        json!("file:///tmp/default.asp")
    );

    write_message(
        &mut stdin,
        &json!({
            "jsonrpc": "2.0",
            "id": 2,
            "method": "shutdown",
            "params": null,
        }),
    );
    let shutdown = read_message(&mut reader);
    assert_eq!(shutdown["id"], json!(2));
    drop(stdin);
    assert!(child.wait().expect("wait server").success());
}

#[test]
fn applies_incremental_change_over_stdio_lsp() {
    let mut child = Command::new(env!("CARGO_BIN_EXE_asp-lsp-server"))
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .spawn()
        .expect("spawn server");

    let mut stdin = child.stdin.take().expect("server stdin");
    let stdout = child.stdout.take().expect("server stdout");
    let mut reader = BufReader::new(stdout);

    initialize(&mut stdin, &mut reader);
    write_message(
        &mut stdin,
        &json!({
            "jsonrpc": "2.0",
            "method": "textDocument/didOpen",
            "params": {
                "textDocument": {
                    "uri": "file:///tmp/default.asp",
                    "languageId": "classic-asp",
                    "version": 1,
                    "text": "<%\nOption Explicit\nDim declaredName\nmissingName = 1\n%>",
                },
            },
        }),
    );
    read_until(&mut reader, |message| {
        message["method"] == json!("textDocument/publishDiagnostics")
            && message.to_string().contains("missingName")
    });

    write_message(
        &mut stdin,
        &json!({
            "jsonrpc": "2.0",
            "method": "textDocument/didChange",
            "params": {
                "textDocument": {
                    "uri": "file:///tmp/default.asp",
                    "version": 2,
                },
                "contentChanges": [{
                    "range": {
                        "start": { "line": 3, "character": 0 },
                        "end": { "line": 3, "character": 11 },
                    },
                    "text": "declaredName",
                }],
            },
        }),
    );

    let diagnostics = read_until(&mut reader, |message| {
        message["method"] == json!("textDocument/publishDiagnostics")
            && message["params"]["uri"] == json!("file:///tmp/default.asp")
            && !message.to_string().contains("missingName")
    });
    assert_eq!(diagnostics["params"]["diagnostics"], json!([]));

    shutdown(&mut stdin, &mut reader);
    drop(stdin);
    assert!(child.wait().expect("wait server").success());
}

#[test]
fn applies_full_replacement_change_over_stdio_lsp() {
    let mut child = Command::new(env!("CARGO_BIN_EXE_asp-lsp-server"))
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .spawn()
        .expect("spawn server");

    let mut stdin = child.stdin.take().expect("server stdin");
    let stdout = child.stdout.take().expect("server stdout");
    let mut reader = BufReader::new(stdout);

    initialize(&mut stdin, &mut reader);
    write_message(
        &mut stdin,
        &json!({
            "jsonrpc": "2.0",
            "method": "textDocument/didOpen",
            "params": {
                "textDocument": {
                    "uri": "file:///tmp/default.asp",
                    "languageId": "classic-asp",
                    "version": 1,
                    "text": "<%\nOption Explicit\nmissingName = 1\n%>",
                },
            },
        }),
    );
    read_until(&mut reader, |message| {
        message["method"] == json!("textDocument/publishDiagnostics")
            && message.to_string().contains("missingName")
    });

    write_message(
        &mut stdin,
        &json!({
            "jsonrpc": "2.0",
            "method": "textDocument/didChange",
            "params": {
                "textDocument": {
                    "uri": "file:///tmp/default.asp",
                    "version": 2,
                },
                "contentChanges": [{
                    "text": "<%\nOption Explicit\nDim declaredName\ndeclaredName = 1\n%>",
                }],
            },
        }),
    );

    let diagnostics = read_until(&mut reader, |message| {
        message["method"] == json!("textDocument/publishDiagnostics")
            && message["params"]["uri"] == json!("file:///tmp/default.asp")
            && !message.to_string().contains("missingName")
    });
    assert_eq!(diagnostics["params"]["diagnostics"], json!([]));

    shutdown(&mut stdin, &mut reader);
    drop(stdin);
    assert!(child.wait().expect("wait server").success());
}

#[test]
fn publishes_fast_parser_then_debounced_vbscript_diagnostics() {
    let mut child = Command::new(env!("CARGO_BIN_EXE_asp-lsp-server"))
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .spawn()
        .expect("spawn server");

    let mut stdin = child.stdin.take().expect("server stdin");
    let stdout = child.stdout.take().expect("server stdout");
    let mut reader = BufReader::new(stdout);

    initialize_with_settings(
        &mut stdin,
        &mut reader,
        json!({ "diagnostics": { "debounceMs": 80 } }),
    );
    write_message(
        &mut stdin,
        &json!({
            "jsonrpc": "2.0",
            "method": "textDocument/didOpen",
            "params": {
                "textDocument": {
                    "uri": "file:///tmp/default.asp",
                    "languageId": "classic-asp",
                    "version": 1,
                    "text": "<%\nOption Explicit\nmissingName = 1\n%>",
                },
            },
        }),
    );

    let fast = read_until(&mut reader, |message| {
        message["method"] == json!("textDocument/publishDiagnostics")
            && message["params"]["uri"] == json!("file:///tmp/default.asp")
    });
    assert_eq!(fast["params"]["diagnostics"], json!([]));

    let semantic = read_until(&mut reader, |message| {
        message["method"] == json!("textDocument/publishDiagnostics")
            && message.to_string().contains("missingName")
    });
    assert_eq!(semantic["params"]["uri"], json!("file:///tmp/default.asp"));

    shutdown(&mut stdin, &mut reader);
    drop(stdin);
    assert!(child.wait().expect("wait server").success());
}

#[test]
fn publishes_embedded_sidecar_diagnostics_over_stdio_lsp() {
    let mut child = Command::new(env!("CARGO_BIN_EXE_asp-lsp-server"))
        .env("ASP_LSP_EMBEDDED_SIDECAR_PATH", embedded_sidecar_path())
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .spawn()
        .expect("spawn server");

    let mut stdin = child.stdin.take().expect("server stdin");
    let stdout = child.stdout.take().expect("server stdout");
    let mut reader = BufReader::new(stdout);

    initialize_with_settings(
        &mut stdin,
        &mut reader,
        json!({ "checkJs": true, "diagnostics": { "debounceMs": 0 } }),
    );
    write_message(
        &mut stdin,
        &json!({
            "jsonrpc": "2.0",
            "method": "textDocument/didOpen",
            "params": {
                "textDocument": {
                    "uri": "file:///tmp/embedded.asp",
                    "languageId": "classic-asp",
                    "version": 1,
                    "text": "<style>.broken { color: }</style>\n<script>missingThing()</script>",
                },
            },
        }),
    );

    let diagnostics = read_until(&mut reader, |message| {
        message["method"] == json!("textDocument/publishDiagnostics")
            && message.to_string().contains("asp-lsp-css")
            && message.to_string().contains("asp-lsp-typescript")
    });
    assert_eq!(
        diagnostics["params"]["uri"],
        json!("file:///tmp/embedded.asp")
    );

    shutdown(&mut stdin, &mut reader);
    drop(stdin);
    assert!(child.wait().expect("wait server").success());
}

#[test]
fn republishes_open_document_diagnostics_after_settings_change() {
    let mut child = Command::new(env!("CARGO_BIN_EXE_asp-lsp-server"))
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .spawn()
        .expect("spawn server");

    let mut stdin = child.stdin.take().expect("server stdin");
    let stdout = child.stdout.take().expect("server stdout");
    let mut reader = BufReader::new(stdout);

    initialize(&mut stdin, &mut reader);
    write_message(
        &mut stdin,
        &json!({
            "jsonrpc": "2.0",
            "method": "textDocument/didOpen",
            "params": {
                "textDocument": {
                    "uri": "file:///tmp/default.asp",
                    "languageId": "classic-asp",
                    "version": 1,
                    "text": "<%\nSub Demo()\nDim unusedValue\nEnd Sub\n%>",
                },
            },
        }),
    );
    read_until(&mut reader, |message| {
        message["method"] == json!("textDocument/publishDiagnostics")
            && message.to_string().contains("unusedValue")
    });

    write_message(
        &mut stdin,
        &json!({
            "jsonrpc": "2.0",
            "method": "workspace/didChangeConfiguration",
            "params": {
                "settings": {
                    "aspLsp": {
                        "vbscript": {
                            "unusedDiagnostics": false,
                        },
                    },
                },
            },
        }),
    );

    let diagnostics = read_until(&mut reader, |message| {
        message["method"] == json!("textDocument/publishDiagnostics")
            && message["params"]["uri"] == json!("file:///tmp/default.asp")
            && !message.to_string().contains("unusedValue")
    });
    assert_eq!(diagnostics["params"]["diagnostics"], json!([]));

    shutdown(&mut stdin, &mut reader);
    drop(stdin);
    assert!(child.wait().expect("wait server").success());
}

#[test]
fn coalesces_debounced_document_changes() {
    let mut child = Command::new(env!("CARGO_BIN_EXE_asp-lsp-server"))
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .spawn()
        .expect("spawn server");

    let mut stdin = child.stdin.take().expect("server stdin");
    let stdout = child.stdout.take().expect("server stdout");
    let mut reader = BufReader::new(stdout);

    initialize_with_settings(
        &mut stdin,
        &mut reader,
        json!({ "diagnostics": { "debounceMs": 80 } }),
    );
    write_message(
        &mut stdin,
        &json!({
            "jsonrpc": "2.0",
            "method": "textDocument/didOpen",
            "params": {
                "textDocument": {
                    "uri": "file:///tmp/default.asp",
                    "languageId": "classic-asp",
                    "version": 1,
                    "text": "<%\nOption Explicit\nmissingName = 1\n%>",
                },
            },
        }),
    );
    read_until(&mut reader, |message| {
        message["method"] == json!("textDocument/publishDiagnostics")
            && message.to_string().contains("missingName")
    });

    for (version, text) in [
        (2, "<%\nOption Explicit\nstaleName = 1\n%>"),
        (
            3,
            "<%\nOption Explicit\nDim declaredName\ndeclaredName = 1\n%>",
        ),
    ] {
        write_message(
            &mut stdin,
            &json!({
                "jsonrpc": "2.0",
                "method": "textDocument/didChange",
                "params": {
                    "textDocument": {
                        "uri": "file:///tmp/default.asp",
                        "version": version,
                    },
                    "contentChanges": [{ "text": text }],
                },
            }),
        );
    }

    let diagnostics = read_message(&mut reader);
    assert_eq!(
        diagnostics["method"],
        json!("textDocument/publishDiagnostics")
    );
    assert_eq!(
        diagnostics["params"]["uri"],
        json!("file:///tmp/default.asp")
    );
    assert!(!diagnostics.to_string().contains("staleName"));
    assert_eq!(diagnostics["params"]["diagnostics"], json!([]));

    shutdown(&mut stdin, &mut reader);
    drop(stdin);
    assert!(child.wait().expect("wait server").success());
}

#[test]
fn serves_vbscript_read_requests_over_stdio_lsp() {
    let mut child = Command::new(env!("CARGO_BIN_EXE_asp-lsp-server"))
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .spawn()
        .expect("spawn server");

    let mut stdin = child.stdin.take().expect("server stdin");
    let stdout = child.stdout.take().expect("server stdout");
    let mut reader = BufReader::new(stdout);
    let uri = "file:///tmp/read.asp";

    initialize(&mut stdin, &mut reader);
    write_message(
        &mut stdin,
        &json!({
            "jsonrpc": "2.0",
            "method": "textDocument/didOpen",
            "params": {
                "textDocument": {
                    "uri": uri,
                    "languageId": "classic-asp",
                    "version": 1,
                    "text": "<%\nFunction BuildName(first)\nBuildName = first\nEnd Function\nDim customerName\ncustomerName = BuildName(\"A\")\n%>",
                },
            },
        }),
    );

    let completion = request(
        &mut stdin,
        &mut reader,
        10,
        "textDocument/completion",
        json!({
            "textDocument": { "uri": uri },
            "position": { "line": 5, "character": 8 },
        }),
    );
    assert!(completion["result"]
        .as_array()
        .expect("completion items")
        .iter()
        .any(|item| item["label"] == json!("customerName")));

    let hover = request(
        &mut stdin,
        &mut reader,
        11,
        "textDocument/hover",
        json!({
            "textDocument": { "uri": uri },
            "position": { "line": 5, "character": 16 },
        }),
    );
    assert!(hover["result"]["contents"]["value"]
        .as_str()
        .expect("hover markdown")
        .contains("Function BuildName(first)"));

    let definition = request(
        &mut stdin,
        &mut reader,
        12,
        "textDocument/definition",
        json!({
            "textDocument": { "uri": uri },
            "position": { "line": 5, "character": 16 },
        }),
    );
    assert_eq!(definition["result"]["uri"], json!(uri));
    assert_eq!(
        definition["result"]["range"]["start"],
        json!({ "line": 1, "character": 9 })
    );

    let signature = request(
        &mut stdin,
        &mut reader,
        13,
        "textDocument/signatureHelp",
        json!({
            "textDocument": { "uri": uri },
            "position": { "line": 5, "character": 26 },
        }),
    );
    assert!(signature["result"]["signatures"][0]["label"]
        .as_str()
        .expect("signature label")
        .contains("BuildName(first)"));
    assert_eq!(signature["result"]["activeParameter"], json!(0));

    let document_symbols = request(
        &mut stdin,
        &mut reader,
        14,
        "textDocument/documentSymbol",
        json!({ "textDocument": { "uri": uri } }),
    );
    assert!(document_symbols["result"]
        .as_array()
        .expect("document symbols")
        .iter()
        .any(|symbol| symbol["name"] == json!("BuildName")));

    let folding_ranges = request(
        &mut stdin,
        &mut reader,
        15,
        "textDocument/foldingRange",
        json!({ "textDocument": { "uri": uri } }),
    );
    assert!(folding_ranges["result"]
        .as_array()
        .expect("folding ranges")
        .iter()
        .any(|range| range["startLine"] == json!(1) && range["endLine"] == json!(3)));

    let highlights = request(
        &mut stdin,
        &mut reader,
        16,
        "textDocument/documentHighlight",
        json!({
            "textDocument": { "uri": uri },
            "position": { "line": 5, "character": 1 },
        }),
    );
    assert!(
        highlights["result"].as_array().expect("highlights").len() >= 2,
        "expected declaration and use highlights"
    );

    shutdown(&mut stdin, &mut reader);
    drop(stdin);
    assert!(child.wait().expect("wait server").success());
}

#[test]
fn serves_embedded_read_requests_over_stdio_lsp() {
    let mut child = Command::new(env!("CARGO_BIN_EXE_asp-lsp-server"))
        .env("ASP_LSP_EMBEDDED_SIDECAR_PATH", embedded_sidecar_path())
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .spawn()
        .expect("spawn server");

    let mut stdin = child.stdin.take().expect("server stdin");
    let stdout = child.stdout.take().expect("server stdout");
    let mut reader = BufReader::new(stdout);
    let uri = "file:///tmp/embedded-read.asp";

    initialize(&mut stdin, &mut reader);
    write_message(
        &mut stdin,
        &json!({
            "jsonrpc": "2.0",
            "method": "textDocument/didOpen",
            "params": {
                "textDocument": {
                    "uri": uri,
                    "languageId": "classic-asp",
                    "version": 1,
                    "text": "<div class=\"box\">\n<style>\n.box {\n  color: red;\n}\n</style>\n</div>",
                },
            },
        }),
    );

    let completion = request(
        &mut stdin,
        &mut reader,
        20,
        "textDocument/completion",
        json!({
            "textDocument": { "uri": uri },
            "position": { "line": 3, "character": 5 },
        }),
    );
    assert!(completion["result"]
        .as_array()
        .expect("embedded completion items")
        .iter()
        .any(|item| item["label"] == json!("color")));

    let hover = request(
        &mut stdin,
        &mut reader,
        21,
        "textDocument/hover",
        json!({
            "textDocument": { "uri": uri },
            "position": { "line": 3, "character": 4 },
        }),
    );
    assert!(hover["result"].to_string().contains("color"));

    let symbols = request(
        &mut stdin,
        &mut reader,
        22,
        "textDocument/documentSymbol",
        json!({ "textDocument": { "uri": uri } }),
    );
    assert!(!symbols["result"]
        .as_array()
        .expect("embedded document symbols")
        .is_empty());

    let folding_ranges = request(
        &mut stdin,
        &mut reader,
        23,
        "textDocument/foldingRange",
        json!({ "textDocument": { "uri": uri } }),
    );
    assert!(
        folding_ranges["result"]
            .as_array()
            .expect("embedded folding ranges")
            .iter()
            .any(|range| range["startLine"] == json!(2) && range["endLine"] == json!(3)),
        "folding ranges: {folding_ranges}"
    );

    let colors = request(
        &mut stdin,
        &mut reader,
        24,
        "textDocument/documentColor",
        json!({ "textDocument": { "uri": uri } }),
    );
    assert!(colors["result"]
        .as_array()
        .expect("embedded colors")
        .iter()
        .any(|color| color["range"]["start"]["line"] == json!(3)));

    let linked = request(
        &mut stdin,
        &mut reader,
        25,
        "textDocument/linkedEditingRange",
        json!({
            "textDocument": { "uri": uri },
            "position": { "line": 0, "character": 2 },
        }),
    );
    let linked_ranges = linked["result"]["ranges"]
        .as_array()
        .expect("linked editing ranges");
    assert!(linked_ranges.len() >= 2);
    assert!(linked_ranges
        .iter()
        .any(|range| range["start"]["line"] == json!(0)));
    assert!(linked_ranges
        .iter()
        .any(|range| range["start"]["line"] == json!(6)));

    shutdown(&mut stdin, &mut reader);
    drop(stdin);
    assert!(child.wait().expect("wait server").success());
}

fn initialize(
    stdin: &mut std::process::ChildStdin,
    reader: &mut BufReader<std::process::ChildStdout>,
) {
    initialize_with_settings(stdin, reader, json!({}));
}

fn initialize_with_settings(
    stdin: &mut std::process::ChildStdin,
    reader: &mut BufReader<std::process::ChildStdout>,
    settings: Value,
) {
    write_message(
        stdin,
        &json!({
            "jsonrpc": "2.0",
            "id": 1,
            "method": "initialize",
            "params": {
                "processId": std::process::id(),
                "rootUri": "file:///tmp",
                "capabilities": {},
                "initializationOptions": {
                    "settings": settings,
                },
            },
        }),
    );
    let initialize = read_message(reader);
    assert_eq!(initialize["id"], json!(1));
    assert_eq!(
        initialize["result"]["serverInfo"]["name"],
        json!("asp-lsp-server")
    );
    assert_eq!(
        initialize["result"]["capabilities"]["textDocumentSync"]["change"],
        json!(2)
    );

    write_message(
        stdin,
        &json!({
            "jsonrpc": "2.0",
            "method": "initialized",
            "params": {},
        }),
    );
}

fn shutdown(
    stdin: &mut std::process::ChildStdin,
    reader: &mut BufReader<std::process::ChildStdout>,
) {
    write_message(
        stdin,
        &json!({
            "jsonrpc": "2.0",
            "id": 2,
            "method": "shutdown",
            "params": null,
        }),
    );
    let shutdown = read_until(reader, |message| message["id"] == json!(2));
    assert_eq!(shutdown["id"], json!(2));
}

fn request(
    stdin: &mut std::process::ChildStdin,
    reader: &mut BufReader<std::process::ChildStdout>,
    id: u64,
    method: &str,
    params: Value,
) -> Value {
    write_message(
        stdin,
        &json!({
            "jsonrpc": "2.0",
            "id": id,
            "method": method,
            "params": params,
        }),
    );
    let response = read_until(reader, |message| message["id"] == json!(id));
    assert!(
        response.get("error").is_none(),
        "{method} returned an error: {response}"
    );
    response
}

fn write_message(stdin: &mut std::process::ChildStdin, message: &Value) {
    let body = message.to_string();
    write!(stdin, "Content-Length: {}\r\n\r\n{}", body.len(), body).expect("write message");
    stdin.flush().expect("flush message");
}

fn read_until(
    reader: &mut BufReader<std::process::ChildStdout>,
    predicate: impl Fn(&Value) -> bool,
) -> Value {
    for _ in 0..20 {
        let message = read_message(reader);
        if predicate(&message) {
            return message;
        }
    }
    panic!("expected message was not published");
}

fn read_message(reader: &mut BufReader<std::process::ChildStdout>) -> Value {
    let mut content_length = None;
    loop {
        let mut line = String::new();
        reader.read_line(&mut line).expect("read header");
        let line = line.trim_end_matches(['\r', '\n']);
        if line.is_empty() {
            break;
        }
        if let Some(value) = line.strip_prefix("Content-Length: ") {
            content_length = Some(value.parse::<usize>().expect("content length"));
        }
    }

    let mut body = vec![0; content_length.expect("content length")];
    std::io::Read::read_exact(reader, &mut body).expect("read body");
    serde_json::from_slice(&body).expect("json message")
}

fn embedded_sidecar_path() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("..")
        .join("..")
        .join("packages")
        .join("embedded-sidecar")
        .join("dist")
        .join("sidecar.js")
}
