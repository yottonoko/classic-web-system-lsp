use serde_json::{json, Value};
use std::io::{BufRead, BufReader, Write};
use std::process::{Command, Stdio};

#[test]
fn jsonl_daemon_handles_multiple_requests_and_errors() {
    let mut child = Command::new(env!("CARGO_BIN_EXE_asp-lsp-core"))
        .arg("--jsonl")
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .spawn()
        .expect("spawn daemon");

    let mut stdin = child.stdin.take().expect("daemon stdin");
    let stdout = child.stdout.take().expect("daemon stdout");
    let mut reader = BufReader::new(stdout);

    writeln!(
        stdin,
        "{}",
        json!({ "id": 1, "request": { "operation": "backendInfo" } })
    )
    .expect("write backend request");
    writeln!(
        stdin,
        "{}",
        json!({ "id": 2, "request": { "operation": "missingOperation" } })
    )
    .expect("write error request");

    let first = read_response(&mut reader);
    let second = read_response(&mut reader);

    assert_eq!(first["id"], json!(1));
    assert_eq!(first["ok"], json!(true));
    assert_eq!(first["result"]["backend"], json!("native"));

    assert_eq!(second["id"], json!(2));
    assert_eq!(second["ok"], json!(false));
    assert!(second["error"]
        .as_str()
        .expect("error message")
        .contains("unknown operation"));

    drop(stdin);
    let status = child.wait().expect("wait daemon");
    assert!(status.success());
}

fn read_response(reader: &mut BufReader<std::process::ChildStdout>) -> Value {
    let mut line = String::new();
    reader.read_line(&mut line).expect("read daemon response");
    serde_json::from_str(&line).expect("parse daemon response")
}
