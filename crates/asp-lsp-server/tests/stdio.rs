use std::io::{BufRead, BufReader, Write};
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
