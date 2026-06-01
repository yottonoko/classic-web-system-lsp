use std::fs;
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
fn refreshes_diagnostics_on_will_save_and_save_hooks() {
    let mut child = Command::new(env!("CARGO_BIN_EXE_asp-lsp-server"))
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .spawn()
        .expect("spawn server");

    let mut stdin = child.stdin.take().expect("server stdin");
    let stdout = child.stdout.take().expect("server stdout");
    let mut reader = BufReader::new(stdout);
    let uri = "file:///tmp/save-hooks.asp";

    initialize_with_settings(
        &mut stdin,
        &mut reader,
        json!({ "format": { "onSave": true } }),
    );
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
                    "text": "<%\nOption Explicit\nmissingName = 1\n%>",
                },
            },
        }),
    );
    read_until(&mut reader, |message| {
        message["method"] == json!("textDocument/publishDiagnostics")
            && message.to_string().contains("missingName")
    });

    let will_save = request(
        &mut stdin,
        &mut reader,
        30,
        "textDocument/willSaveWaitUntil",
        json!({
            "textDocument": { "uri": uri },
            "reason": 1,
        }),
    );
    assert_eq!(will_save["result"], json!([]));

    write_message(
        &mut stdin,
        &json!({
            "jsonrpc": "2.0",
            "method": "textDocument/didSave",
            "params": {
                "textDocument": { "uri": uri },
                "text": "<%\nOption Explicit\nDim declaredName\ndeclaredName = 1\n%>",
            },
        }),
    );

    let diagnostics = read_until(&mut reader, |message| {
        message["method"] == json!("textDocument/publishDiagnostics")
            && message["params"]["uri"] == json!(uri)
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
                    "text": "<%\nFunction BuildName(first)\nBuildName=first\nEnd Function\nDim customerName\ncustomerName = BuildName(\"A\")\nSub RenderName()\nResponse.Write BuildName(\"B\")\nEnd Sub\nClass Customer\nPublic Function DisplayName()\nDisplayName = BuildName(\"C\")\nEnd Function\nPublic Sub RenderSelf()\nMe.DisplayName()\nEnd Sub\nEnd Class\nSub RenderTyped()\nDim typedCustomer\nSet typedCustomer = New Customer\ntypedCustomer.DisplayName()\nEnd Sub\nSub LocalOne()\nDim scopedName\nscopedName = \"A\"\nEnd Sub\nSub LocalTwo()\nDim scopedName\nscopedName = \"B\"\nEnd Sub\nDim repeatedName\nSub ParamScope(repeatedName)\nResponse.Write repeatedName\nEnd Sub\nFunction MakeText()\nMakeText = \"value\"\nEnd Function\n%>",
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

    let builtin_completion = request(
        &mut stdin,
        &mut reader,
        45,
        "textDocument/completion",
        json!({
            "textDocument": { "uri": uri },
            "position": { "line": 4, "character": 0 },
        }),
    );
    let date_part = builtin_completion["result"]
        .as_array()
        .expect("builtin completion items")
        .iter()
        .find(|item| item["label"] == json!("DatePart"))
        .expect("DatePart completion")
        .clone();
    let resolved_date_part = request(
        &mut stdin,
        &mut reader,
        46,
        "completionItem/resolve",
        date_part,
    );
    assert_eq!(
        resolved_date_part["result"]["detail"],
        json!("Function DatePart(interval, date, firstDayOfWeek, firstWeekOfYear) As Number"),
        "resolved DatePart: {resolved_date_part}"
    );
    assert!(resolved_date_part["result"]["documentation"]["value"]
        .as_str()
        .expect("DatePart documentation")
        .contains("Returns part of a date."));

    let member_completion = request(
        &mut stdin,
        &mut reader,
        47,
        "textDocument/completion",
        json!({
            "textDocument": { "uri": uri },
            "position": { "line": 7, "character": 9 },
        }),
    );
    let response_write = member_completion["result"]
        .as_array()
        .expect("Response member completion items")
        .iter()
        .find(|item| item["label"] == json!("Write"))
        .expect("Response.Write completion")
        .clone();
    let resolved_response_write = request(
        &mut stdin,
        &mut reader,
        48,
        "completionItem/resolve",
        response_write,
    );
    assert_eq!(
        resolved_response_write["result"]["detail"],
        json!("method As Variant"),
        "resolved Response.Write: {resolved_response_write}"
    );
    assert!(resolved_response_write["result"]["documentation"]["value"]
        .as_str()
        .expect("Response.Write documentation")
        .contains("Response.Write value"));

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

    let type_definition = request(
        &mut stdin,
        &mut reader,
        43,
        "textDocument/typeDefinition",
        json!({
            "textDocument": { "uri": uri },
            "position": { "line": 20, "character": 2 },
        }),
    );
    assert_eq!(type_definition["result"]["uri"], json!(uri));
    assert_eq!(
        type_definition["result"]["range"]["start"],
        json!({ "line": 9, "character": 6 })
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
    let document_symbol_items = document_symbols["result"]
        .as_array()
        .expect("document symbols");
    assert!(document_symbol_items
        .iter()
        .any(|symbol| symbol["name"] == json!("BuildName")));
    assert!(document_symbol_items
        .iter()
        .any(|symbol| symbol["name"] == json!("Customer.DisplayName")));
    assert!(!document_symbol_items
        .iter()
        .any(|symbol| symbol["name"] == json!("customerName")));

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

    let references = request(
        &mut stdin,
        &mut reader,
        17,
        "textDocument/references",
        json!({
            "textDocument": { "uri": uri },
            "position": { "line": 5, "character": 16 },
            "context": { "includeDeclaration": true },
        }),
    );
    assert!(
        references["result"].as_array().expect("references").len() >= 2,
        "expected declaration and use references"
    );
    let scoped_references = request(
        &mut stdin,
        &mut reader,
        40,
        "textDocument/references",
        json!({
            "textDocument": { "uri": uri },
            "position": { "line": 24, "character": 1 },
            "context": { "includeDeclaration": true },
        }),
    );
    let scoped_reference_items = scoped_references["result"]
        .as_array()
        .expect("scoped references");
    assert_eq!(scoped_reference_items.len(), 2);
    assert!(!scoped_reference_items.iter().any(|reference| {
        matches!(reference["range"]["start"]["line"].as_u64(), Some(27 | 28))
    }));
    let second_scoped_references = request(
        &mut stdin,
        &mut reader,
        42,
        "textDocument/references",
        json!({
            "textDocument": { "uri": uri },
            "position": { "line": 28, "character": 1 },
            "context": { "includeDeclaration": true },
        }),
    );
    let second_scoped_reference_items = second_scoped_references["result"]
        .as_array()
        .expect("second scoped references");
    assert_eq!(second_scoped_reference_items.len(), 2);
    assert!(!second_scoped_reference_items.iter().any(|reference| {
        matches!(reference["range"]["start"]["line"].as_u64(), Some(23 | 24))
    }));

    let prepare_rename = request(
        &mut stdin,
        &mut reader,
        18,
        "textDocument/prepareRename",
        json!({
            "textDocument": { "uri": uri },
            "position": { "line": 5, "character": 16 },
        }),
    );
    assert_eq!(
        prepare_rename["result"]["start"],
        json!({ "line": 5, "character": 15 })
    );

    let rename = request(
        &mut stdin,
        &mut reader,
        19,
        "textDocument/rename",
        json!({
            "textDocument": { "uri": uri },
            "position": { "line": 5, "character": 16 },
            "newName": "FormatName",
        }),
    );
    assert!(rename["result"]["changes"][uri]
        .as_array()
        .expect("rename edits")
        .iter()
        .any(|edit| edit["newText"] == json!("FormatName")));
    let scoped_rename = request(
        &mut stdin,
        &mut reader,
        41,
        "textDocument/rename",
        json!({
            "textDocument": { "uri": uri },
            "position": { "line": 24, "character": 1 },
            "newName": "scopedValue",
        }),
    );
    let scoped_rename_edits = scoped_rename["result"]["changes"][uri]
        .as_array()
        .expect("scoped rename edits");
    assert_eq!(scoped_rename_edits.len(), 2);
    assert!(!scoped_rename_edits
        .iter()
        .any(|edit| { matches!(edit["range"]["start"]["line"].as_u64(), Some(27 | 28)) }));
    let second_scoped_rename = request(
        &mut stdin,
        &mut reader,
        43,
        "textDocument/rename",
        json!({
            "textDocument": { "uri": uri },
            "position": { "line": 28, "character": 1 },
            "newName": "secondScopedValue",
        }),
    );
    let second_scoped_rename_edits = second_scoped_rename["result"]["changes"][uri]
        .as_array()
        .expect("second scoped rename edits");
    assert_eq!(second_scoped_rename_edits.len(), 2);
    assert!(!second_scoped_rename_edits
        .iter()
        .any(|edit| { matches!(edit["range"]["start"]["line"].as_u64(), Some(23 | 24)) }));

    let workspace_symbols = request(
        &mut stdin,
        &mut reader,
        20,
        "workspace/symbol",
        json!({ "query": "Build" }),
    );
    assert!(workspace_symbols["result"]
        .as_array()
        .expect("workspace symbols")
        .iter()
        .any(|symbol| symbol["name"] == json!("BuildName")));

    let semantic_tokens = request(
        &mut stdin,
        &mut reader,
        21,
        "textDocument/semanticTokens/full",
        json!({ "textDocument": { "uri": uri } }),
    );
    let decoded = decode_semantic_tokens(
        semantic_tokens["result"]["data"]
            .as_array()
            .expect("semantic token data"),
    );
    assert!(decoded
        .iter()
        .any(|token| token.line == 5 && token.character == 15 && token.token_type == 3));
    assert!(decoded
        .iter()
        .any(|token| token.line == 1 && token.character == 19 && token.token_type == 2));
    assert!(decoded
        .iter()
        .any(|token| token.line == 32 && token.character == 15 && token.token_type == 2));
    let semantic_delta = request(
        &mut stdin,
        &mut reader,
        22,
        "textDocument/semanticTokens/full/delta",
        json!({
            "textDocument": { "uri": uri },
            "previousResultId": semantic_tokens["result"]["resultId"],
        }),
    );
    assert_ne!(
        semantic_delta["result"]["resultId"],
        semantic_tokens["result"]["resultId"]
    );
    assert_eq!(
        semantic_delta["result"]["edits"][0],
        json!({
            "start": semantic_tokens["result"]["data"].as_array().expect("semantic data").len(),
            "deleteCount": 0,
            "data": [],
        })
    );

    let selection = request(
        &mut stdin,
        &mut reader,
        23,
        "textDocument/selectionRange",
        json!({
            "textDocument": { "uri": uri },
            "positions": [
                { "line": 5, "character": 16 },
                { "line": 2, "character": 2 },
            ],
        }),
    );
    assert_eq!(
        selection["result"][0]["range"]["start"],
        json!({ "line": 5, "character": 15 })
    );
    assert_eq!(
        selection["result"][1]["parent"]["parent"]["range"]["start"],
        json!({ "line": 1, "character": 0 })
    );
    assert_eq!(
        selection["result"][1]["parent"]["parent"]["range"]["end"]["line"],
        json!(3)
    );

    let inlay_hints = request(
        &mut stdin,
        &mut reader,
        24,
        "textDocument/inlayHint",
        json!({
            "textDocument": { "uri": uri },
            "range": {
                "start": { "line": 0, "character": 0 },
                "end": { "line": 40, "character": 0 },
            },
        }),
    );
    let inlay_labels = inlay_hints["result"]
        .as_array()
        .expect("inlay hints")
        .iter()
        .filter_map(|hint| hint["label"].as_str())
        .collect::<Vec<_>>();
    assert!(
        inlay_labels.iter().any(|label| *label == "first:"),
        "inlay labels: {inlay_labels:?}"
    );
    assert!(
        inlay_labels.iter().any(|label| *label == "ByRef "),
        "inlay labels: {inlay_labels:?}"
    );
    assert!(
        inlay_labels
            .iter()
            .any(|label| label.contains(" As Customer")),
        "inlay labels: {inlay_labels:?}"
    );
    assert!(
        inlay_labels.iter().any(|label| *label == " As String"),
        "inlay labels: {inlay_labels:?}"
    );

    write_message(
        &mut stdin,
        &json!({
            "jsonrpc": "2.0",
            "method": "workspace/didChangeConfiguration",
            "params": {
                "settings": {
                    "aspLsp": {
                        "inlayHints": {
                            "variableTypes": false,
                            "parameterNames": false,
                            "functionReturnTypes": false,
                            "implicitByRef": false,
                        },
                    },
                },
            },
        }),
    );
    let disabled_inlay_hints = request(
        &mut stdin,
        &mut reader,
        44,
        "textDocument/inlayHint",
        json!({
            "textDocument": { "uri": uri },
            "range": {
                "start": { "line": 0, "character": 0 },
                "end": { "line": 40, "character": 0 },
            },
        }),
    );
    assert!(disabled_inlay_hints["result"]
        .as_array()
        .expect("disabled inlay hints")
        .is_empty());

    let code_lens = request(
        &mut stdin,
        &mut reader,
        25,
        "textDocument/codeLens",
        json!({ "textDocument": { "uri": uri } }),
    );
    assert!(code_lens["result"]
        .as_array()
        .expect("code lens")
        .iter()
        .any(|lens| lens["data"]["kind"] == json!("vbscript-reference")));
    let resolved_lens = request(
        &mut stdin,
        &mut reader,
        26,
        "codeLens/resolve",
        code_lens["result"][0].clone(),
    );
    assert!(resolved_lens["result"].to_string().contains("references"));

    let actions = request(
        &mut stdin,
        &mut reader,
        27,
        "textDocument/codeAction",
        json!({
            "textDocument": { "uri": uri },
            "range": {
                "start": { "line": 5, "character": 15 },
                "end": { "line": 5, "character": 24 },
            },
            "context": { "diagnostics": [] },
        }),
    );
    assert!(actions["result"]
        .to_string()
        .contains("Extract VBScript variable"));

    let documentation_actions = request(
        &mut stdin,
        &mut reader,
        47,
        "textDocument/codeAction",
        json!({
            "textDocument": { "uri": uri },
            "range": {
                "start": { "line": 1, "character": 9 },
                "end": { "line": 1, "character": 18 },
            },
            "context": { "diagnostics": [] },
        }),
    );
    let documentation_action = documentation_actions["result"]
        .as_array()
        .expect("documentation actions")
        .iter()
        .find(|action| action["title"] == json!("Generate VBScript documentation"))
        .expect("documentation code action");
    let documentation_edit = &documentation_action["edit"]["changes"][uri][0];
    assert_eq!(
        documentation_edit["range"],
        json!({
            "start": { "line": 1, "character": 0 },
            "end": { "line": 1, "character": 0 },
        })
    );
    let documentation_text = documentation_edit["newText"]
        .as_str()
        .expect("documentation new text");
    assert!(documentation_text.contains("' @param BuildName.first As Variant"));
    assert!(documentation_text.contains("' @returns BuildName Variant"));
    assert!(documentation_text.contains("''' <summary>TODO: Describe BuildName.</summary>"));
    assert!(documentation_text.contains("''' <param name=\"first\">TODO: Describe first.</param>"));
    assert!(documentation_text.contains("''' <returns>TODO: Describe return value.</returns>"));

    let variable_documentation_actions = request(
        &mut stdin,
        &mut reader,
        48,
        "textDocument/codeAction",
        json!({
            "textDocument": { "uri": uri },
            "range": {
                "start": { "line": 4, "character": 4 },
                "end": { "line": 4, "character": 16 },
            },
            "context": { "diagnostics": [] },
        }),
    );
    let variable_documentation_text =
        documentation_action_new_text(&variable_documentation_actions, uri);
    assert!(variable_documentation_text.contains("' @type customerName As Variant"));
    assert!(
        variable_documentation_text.contains("''' <summary>TODO: Describe customerName.</summary>")
    );
    assert!(variable_documentation_text.contains("''' <value>TODO: Describe customerName.</value>"));

    let parameter_documentation_actions = request(
        &mut stdin,
        &mut reader,
        49,
        "textDocument/codeAction",
        json!({
            "textDocument": { "uri": uri },
            "range": {
                "start": { "line": 31, "character": 15 },
                "end": { "line": 31, "character": 27 },
            },
            "context": { "diagnostics": [] },
        }),
    );
    let parameter_documentation_text =
        documentation_action_new_text(&parameter_documentation_actions, uri);
    assert!(parameter_documentation_text.contains("' @param ParamScope.repeatedName As Variant"));
    assert!(parameter_documentation_text
        .contains("''' <param name=\"repeatedName\">TODO: Describe repeatedName.</param>"));

    let call_hierarchy = request(
        &mut stdin,
        &mut reader,
        28,
        "textDocument/prepareCallHierarchy",
        json!({
            "textDocument": { "uri": uri },
            "position": { "line": 5, "character": 16 },
        }),
    );
    assert!(call_hierarchy["result"].to_string().contains("BuildName"));
    let variable_call_hierarchy = request(
        &mut stdin,
        &mut reader,
        38,
        "textDocument/prepareCallHierarchy",
        json!({
            "textDocument": { "uri": uri },
            "position": { "line": 4, "character": 5 },
        }),
    );
    assert_eq!(variable_call_hierarchy["result"], json!([]));
    let incoming_calls = request(
        &mut stdin,
        &mut reader,
        29,
        "callHierarchy/incomingCalls",
        json!({ "item": call_hierarchy["result"][0].clone() }),
    );
    assert!(incoming_calls["result"].to_string().contains("RenderName"));
    assert!(!incoming_calls["result"]
        .as_array()
        .expect("incoming calls")
        .iter()
        .any(|call| call["from"]["name"] == json!("BuildName")));

    let render_hierarchy = request(
        &mut stdin,
        &mut reader,
        36,
        "textDocument/prepareCallHierarchy",
        json!({
            "textDocument": { "uri": uri },
            "position": { "line": 6, "character": 5 },
        }),
    );
    assert!(render_hierarchy["result"]
        .to_string()
        .contains("RenderName"));
    let outgoing_calls = request(
        &mut stdin,
        &mut reader,
        37,
        "callHierarchy/outgoingCalls",
        json!({ "item": render_hierarchy["result"][0].clone() }),
    );
    assert!(outgoing_calls["result"].to_string().contains("BuildName"));
    let display_hierarchy = request(
        &mut stdin,
        &mut reader,
        44,
        "textDocument/prepareCallHierarchy",
        json!({
            "textDocument": { "uri": uri },
            "position": { "line": 10, "character": 18 },
        }),
    );
    assert!(display_hierarchy["result"]
        .to_string()
        .contains("Customer.DisplayName"));
    let display_incoming_calls = request(
        &mut stdin,
        &mut reader,
        45,
        "callHierarchy/incomingCalls",
        json!({ "item": display_hierarchy["result"][0].clone() }),
    );
    assert!(display_incoming_calls["result"]
        .to_string()
        .contains("Customer.RenderSelf"));
    assert!(display_incoming_calls["result"]
        .to_string()
        .contains("RenderTyped"));
    let self_hierarchy = request(
        &mut stdin,
        &mut reader,
        46,
        "textDocument/prepareCallHierarchy",
        json!({
            "textDocument": { "uri": uri },
            "position": { "line": 13, "character": 13 },
        }),
    );
    let self_outgoing_calls = request(
        &mut stdin,
        &mut reader,
        47,
        "callHierarchy/outgoingCalls",
        json!({ "item": self_hierarchy["result"][0].clone() }),
    );
    assert!(self_outgoing_calls["result"]
        .to_string()
        .contains("Customer.DisplayName"));
    let typed_hierarchy = request(
        &mut stdin,
        &mut reader,
        48,
        "textDocument/prepareCallHierarchy",
        json!({
            "textDocument": { "uri": uri },
            "position": { "line": 17, "character": 5 },
        }),
    );
    let typed_outgoing_calls = request(
        &mut stdin,
        &mut reader,
        49,
        "callHierarchy/outgoingCalls",
        json!({ "item": typed_hierarchy["result"][0].clone() }),
    );
    assert!(typed_outgoing_calls["result"]
        .to_string()
        .contains("Customer.DisplayName"));

    let type_hierarchy = request(
        &mut stdin,
        &mut reader,
        30,
        "textDocument/prepareTypeHierarchy",
        json!({
            "textDocument": { "uri": uri },
            "position": { "line": 9, "character": 8 },
        }),
    );
    assert!(type_hierarchy["result"].to_string().contains("Customer"));
    let callable_type_hierarchy = request(
        &mut stdin,
        &mut reader,
        39,
        "textDocument/prepareTypeHierarchy",
        json!({
            "textDocument": { "uri": uri },
            "position": { "line": 5, "character": 16 },
        }),
    );
    assert_eq!(callable_type_hierarchy["result"], json!([]));

    let monikers = request(
        &mut stdin,
        &mut reader,
        31,
        "textDocument/moniker",
        json!({
            "textDocument": { "uri": uri },
            "position": { "line": 5, "character": 16 },
        }),
    );
    assert!(monikers["result"].to_string().contains("BuildName"));

    let member_monikers = request(
        &mut stdin,
        &mut reader,
        50,
        "textDocument/moniker",
        json!({
            "textDocument": { "uri": uri },
            "position": { "line": 10, "character": 17 },
        }),
    );
    assert!(member_monikers["result"][0]["identifier"]
        .as_str()
        .expect("member moniker identifier")
        .contains("Customer.DisplayName"));

    let first_local_moniker = request(
        &mut stdin,
        &mut reader,
        51,
        "textDocument/moniker",
        json!({
            "textDocument": { "uri": uri },
            "position": { "line": 24, "character": 1 },
        }),
    );
    let second_local_moniker = request(
        &mut stdin,
        &mut reader,
        52,
        "textDocument/moniker",
        json!({
            "textDocument": { "uri": uri },
            "position": { "line": 28, "character": 1 },
        }),
    );
    assert_eq!(first_local_moniker["result"][0]["kind"], json!("local"));
    assert_ne!(
        first_local_moniker["result"][0]["identifier"],
        second_local_moniker["result"][0]["identifier"]
    );

    let inline_values = request(
        &mut stdin,
        &mut reader,
        32,
        "textDocument/inlineValue",
        json!({
            "textDocument": { "uri": uri },
            "range": {
                "start": { "line": 1, "character": 0 },
                "end": { "line": 6, "character": 0 },
            },
            "context": {
                "frameId": 0,
                "stoppedLocation": {
                    "start": { "line": 5, "character": 0 },
                    "end": { "line": 5, "character": 0 },
                },
            },
        }),
    );
    assert!(inline_values["result"].to_string().contains("BuildName"));

    let formatting = request(
        &mut stdin,
        &mut reader,
        33,
        "textDocument/formatting",
        json!({
            "textDocument": { "uri": uri },
            "options": { "tabSize": 2, "insertSpaces": true },
        }),
    );
    assert!(formatting["result"][0]["newText"]
        .as_str()
        .expect("formatted text")
        .contains("  Response.Write BuildName(\"B\")"));

    let range_formatting = request(
        &mut stdin,
        &mut reader,
        34,
        "textDocument/rangeFormatting",
        json!({
            "textDocument": { "uri": uri },
            "range": {
                "start": { "line": 1, "character": 0 },
                "end": { "line": 6, "character": 0 },
            },
            "options": { "tabSize": 2, "insertSpaces": true },
        }),
    );
    assert!(range_formatting["result"][0]["newText"]
        .as_str()
        .expect("range formatted text")
        .contains("BuildName = first"));

    let on_type_formatting = request(
        &mut stdin,
        &mut reader,
        35,
        "textDocument/onTypeFormatting",
        json!({
            "textDocument": { "uri": uri },
            "position": { "line": 2, "character": 15 },
            "ch": ">",
            "options": { "tabSize": 2, "insertSpaces": true },
        }),
    );
    let on_type_text = on_type_formatting["result"][0]["newText"]
        .as_str()
        .expect("on type formatted text");
    assert_eq!(on_type_text, "BuildName = first");
    assert!(!on_type_text.contains("<%"));

    shutdown(&mut stdin, &mut reader);
    drop(stdin);
    assert!(child.wait().expect("wait server").success());
}

#[test]
fn omits_vbscript_documentation_action_for_documented_callable_over_stdio_lsp() {
    let mut child = Command::new(env!("CARGO_BIN_EXE_asp-lsp-server"))
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .spawn()
        .expect("spawn server");

    let mut stdin = child.stdin.take().expect("server stdin");
    let stdout = child.stdout.take().expect("server stdout");
    let mut reader = BufReader::new(stdout);
    let uri = "file:///tmp/documented.asp";

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
                    "text": "<%\n''' <summary>Already documented.</summary>\n''' <param name=\"first\">Already documented.</param>\n''' <returns>Already documented.</returns>\n' @param DocumentedName.first As Variant\n' @returns DocumentedName Variant\nFunction DocumentedName(first)\nDocumentedName = first\nEnd Function\n%>",
                },
            },
        }),
    );

    let actions = request(
        &mut stdin,
        &mut reader,
        3,
        "textDocument/codeAction",
        json!({
            "textDocument": { "uri": uri },
            "range": {
                "start": { "line": 6, "character": 9 },
                "end": { "line": 6, "character": 23 },
            },
            "context": { "diagnostics": [] },
        }),
    );
    assert!(!actions["result"]
        .as_array()
        .expect("code actions")
        .iter()
        .any(|action| action["title"] == json!("Generate VBScript documentation")));

    shutdown(&mut stdin, &mut reader);
    drop(stdin);
    assert!(child.wait().expect("wait server").success());
}

#[test]
fn generates_vbscript_documentation_for_class_and_property_over_stdio_lsp() {
    let mut child = Command::new(env!("CARGO_BIN_EXE_asp-lsp-server"))
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .spawn()
        .expect("spawn server");

    let mut stdin = child.stdin.take().expect("server stdin");
    let stdout = child.stdout.take().expect("server stdout");
    let mut reader = BufReader::new(stdout);
    let uri = "file:///tmp/broad-docs.asp";

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
                    "text": "<%\nClass Customer\n  Public Name\n  Public Property Get DisplayName()\n    DisplayName = Name\n  End Property\nEnd Class\n%>",
                },
            },
        }),
    );

    let class_actions = request(
        &mut stdin,
        &mut reader,
        3,
        "textDocument/codeAction",
        json!({
            "textDocument": { "uri": uri },
            "range": {
                "start": { "line": 1, "character": 6 },
                "end": { "line": 1, "character": 14 },
            },
            "context": { "diagnostics": [] },
        }),
    );
    let class_documentation_text = documentation_action_new_text(&class_actions, uri);
    assert!(class_documentation_text.contains("''' <summary>TODO: Describe Customer.</summary>"));

    let property_actions = request(
        &mut stdin,
        &mut reader,
        4,
        "textDocument/codeAction",
        json!({
            "textDocument": { "uri": uri },
            "range": {
                "start": { "line": 3, "character": 22 },
                "end": { "line": 3, "character": 33 },
            },
            "context": { "diagnostics": [] },
        }),
    );
    let property_documentation_text = documentation_action_new_text(&property_actions, uri);
    assert!(property_documentation_text.contains("' @returns DisplayName Variant"));
    assert!(
        property_documentation_text.contains("''' <summary>TODO: Describe DisplayName.</summary>")
    );
    assert!(
        property_documentation_text.contains("''' <returns>TODO: Describe return value.</returns>")
    );
    assert!(property_documentation_text.contains("''' <value>TODO: Describe DisplayName.</value>"));

    shutdown(&mut stdin, &mut reader);
    drop(stdin);
    assert!(child.wait().expect("wait server").success());
}

#[test]
fn localizes_vbscript_code_action_and_codelens_titles_over_stdio_lsp() {
    let mut child = Command::new(env!("CARGO_BIN_EXE_asp-lsp-server"))
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .spawn()
        .expect("spawn server");

    let mut stdin = child.stdin.take().expect("server stdin");
    let stdout = child.stdout.take().expect("server stdout");
    let mut reader = BufReader::new(stdout);
    let uri = "file:///tmp/localized.asp";

    initialize_with_settings(
        &mut stdin,
        &mut reader,
        json!({ "locale": "ja", "codeLens": { "includes": true } }),
    );
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
                    "text": "<!-- #include file=\"helpers.inc\" -->\n<%\nFunction BuildName(first)\nBuildName = first\nEnd Function\nDim customerName\ncustomerName = BuildName(\"A\")\n%>",
                },
            },
        }),
    );

    let documentation_actions = request(
        &mut stdin,
        &mut reader,
        3,
        "textDocument/codeAction",
        json!({
            "textDocument": { "uri": uri },
            "range": {
                "start": { "line": 2, "character": 9 },
                "end": { "line": 2, "character": 18 },
            },
            "context": { "diagnostics": [] },
        }),
    );
    assert!(documentation_actions["result"]
        .as_array()
        .expect("documentation actions")
        .iter()
        .any(|action| action["title"] == json!("VBScript documentation を生成")));

    let extract_actions = request(
        &mut stdin,
        &mut reader,
        4,
        "textDocument/codeAction",
        json!({
            "textDocument": { "uri": uri },
            "range": {
                "start": { "line": 6, "character": 15 },
                "end": { "line": 6, "character": 24 },
            },
            "context": { "diagnostics": [] },
        }),
    );
    assert!(extract_actions["result"]
        .as_array()
        .expect("extract actions")
        .iter()
        .any(|action| action["title"] == json!("VBScript 変数に抽出")));

    let code_lens = request(
        &mut stdin,
        &mut reader,
        5,
        "textDocument/codeLens",
        json!({ "textDocument": { "uri": uri } }),
    );
    let code_lens_items = code_lens["result"].as_array().expect("code lens");
    assert!(code_lens_items
        .iter()
        .any(|lens| lens["command"]["title"] == json!("helpers.inc を include")));
    let reference_lens = code_lens_items
        .iter()
        .find(|lens| lens["data"]["kind"] == json!("vbscript-reference"))
        .expect("reference lens")
        .clone();
    let resolved_lens = request(
        &mut stdin,
        &mut reader,
        6,
        "codeLens/resolve",
        reference_lens,
    );
    assert!(resolved_lens["result"]["command"]["title"]
        .as_str()
        .expect("localized reference title")
        .contains("件の参照"));

    shutdown(&mut stdin, &mut reader);
    drop(stdin);
    assert!(child.wait().expect("wait server").success());
}

#[test]
fn respects_vbscript_codelens_settings_over_stdio_lsp() {
    let mut child = Command::new(env!("CARGO_BIN_EXE_asp-lsp-server"))
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .spawn()
        .expect("spawn server");

    let mut stdin = child.stdin.take().expect("server stdin");
    let stdout = child.stdout.take().expect("server stdout");
    let mut reader = BufReader::new(stdout);
    let uri = "file:///tmp/codelens-settings.asp";

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
                    "text": "<!-- #include file=\"helpers.inc\" -->\n<%\nFunction BuildName(first)\nBuildName = first\nEnd Function\nResponse.Write BuildName(\"A\")\n%>",
                },
            },
        }),
    );

    let default_code_lens = request(
        &mut stdin,
        &mut reader,
        3,
        "textDocument/codeLens",
        json!({ "textDocument": { "uri": uri } }),
    );
    let default_code_lens_items = default_code_lens["result"].as_array().expect("code lens");
    assert!(default_code_lens_items
        .iter()
        .any(|lens| lens["data"]["kind"] == json!("vbscript-reference")));
    assert!(!default_code_lens_items
        .iter()
        .any(|lens| lens["command"]["title"] == json!("include helpers.inc")));

    write_message(
        &mut stdin,
        &json!({
            "jsonrpc": "2.0",
            "method": "workspace/didChangeConfiguration",
            "params": {
                "settings": { "codeLens": { "references": false, "includes": true } },
            },
        }),
    );

    let configured_code_lens = request(
        &mut stdin,
        &mut reader,
        4,
        "textDocument/codeLens",
        json!({ "textDocument": { "uri": uri } }),
    );
    let code_lens_items = configured_code_lens["result"]
        .as_array()
        .expect("code lens");
    assert!(code_lens_items
        .iter()
        .any(|lens| lens["command"]["title"] == json!("include helpers.inc")));
    assert!(!code_lens_items
        .iter()
        .any(|lens| lens["data"]["kind"] == json!("vbscript-reference")));

    shutdown(&mut stdin, &mut reader);
    drop(stdin);
    assert!(child.wait().expect("wait server").success());
}

#[test]
fn aligns_vbscript_assignments_over_stdio_lsp() {
    let mut child = Command::new(env!("CARGO_BIN_EXE_asp-lsp-server"))
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .spawn()
        .expect("spawn server");

    let mut stdin = child.stdin.take().expect("server stdin");
    let stdout = child.stdout.take().expect("server stdout");
    let mut reader = BufReader::new(stdout);
    let uri = "file:///tmp/align.asp";

    initialize_with_settings(
        &mut stdin,
        &mut reader,
        json!({ "format": { "alignAssignments": true } }),
    );
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
                    "text": "<%\nDim first\nDim longerName\nfirst=1\nlongerName=2\n%>",
                },
            },
        }),
    );

    let formatting = request(
        &mut stdin,
        &mut reader,
        10,
        "textDocument/formatting",
        json!({
            "textDocument": { "uri": uri },
            "options": { "tabSize": 2, "insertSpaces": true },
        }),
    );
    assert!(formatting["result"][0]["newText"]
        .as_str()
        .expect("formatted text")
        .contains("first      = 1\nlongerName = 2"));

    shutdown(&mut stdin, &mut reader);
    drop(stdin);
    assert!(child.wait().expect("wait server").success());
}

#[test]
fn indexes_unopened_workspace_files_for_vbscript_references() {
    let root = std::env::temp_dir().join(format!("asp-lsp-rust-index-{}", std::process::id()));
    let _ = fs::remove_dir_all(&root);
    fs::create_dir_all(&root).expect("create temp root");
    let include = root.join("helpers.inc");
    fs::write(
        &include,
        "<%\nFunction BuildName(first)\nBuildName = first\nEnd Function\n%>",
    )
    .expect("write include");

    let mut child = Command::new(env!("CARGO_BIN_EXE_asp-lsp-server"))
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .spawn()
        .expect("spawn server");

    let mut stdin = child.stdin.take().expect("server stdin");
    let stdout = child.stdout.take().expect("server stdout");
    let mut reader = BufReader::new(stdout);
    let root_uri = format!("file://{}", root.to_string_lossy());
    let page_uri = format!("{root_uri}/default.asp");

    initialize_with_root(&mut stdin, &mut reader, &root_uri);
    write_message(
        &mut stdin,
        &json!({
            "jsonrpc": "2.0",
            "method": "textDocument/didOpen",
            "params": {
                "textDocument": {
                    "uri": page_uri,
                    "languageId": "classic-asp",
                    "version": 1,
                    "text": "<!-- #include file=\"helpers.inc\" -->\n<%\nResponse.Write BuildName(\"Ada\")\n%>",
                },
            },
        }),
    );
    read_until(&mut reader, |message| {
        message["method"] == json!("textDocument/publishDiagnostics")
    });

    let references = request(
        &mut stdin,
        &mut reader,
        30,
        "textDocument/references",
        json!({
            "textDocument": { "uri": page_uri },
            "position": { "line": 2, "character": 16 },
            "context": { "includeDeclaration": true },
        }),
    );
    let serialized = references.to_string();
    assert!(
        serialized.contains("helpers.inc"),
        "references: {serialized}"
    );

    let links = request(
        &mut stdin,
        &mut reader,
        31,
        "textDocument/documentLink",
        json!({ "textDocument": { "uri": page_uri } }),
    );
    assert!(links["result"].to_string().contains("helpers.inc"));

    let resolved_link = request(
        &mut stdin,
        &mut reader,
        32,
        "documentLink/resolve",
        links["result"][0].clone(),
    );
    assert!(resolved_link["result"].to_string().contains("helpers.inc"));

    shutdown(&mut stdin, &mut reader);
    drop(stdin);
    assert!(child.wait().expect("wait server").success());
    let _ = fs::remove_dir_all(root);
}

#[test]
fn refreshes_workspace_index_after_file_operations() {
    let root = std::env::temp_dir().join(format!(
        "asp-lsp-rust-file-operations-{}",
        std::process::id()
    ));
    let _ = fs::remove_dir_all(&root);
    fs::create_dir_all(&root).expect("create temp root");

    let mut child = Command::new(env!("CARGO_BIN_EXE_asp-lsp-server"))
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .spawn()
        .expect("spawn server");

    let mut stdin = child.stdin.take().expect("server stdin");
    let stdout = child.stdout.take().expect("server stdout");
    let mut reader = BufReader::new(stdout);
    let root_uri = format!("file://{}", root.to_string_lossy());
    let include_uri = format!("{root_uri}/helpers.inc");
    let page_uri = format!("{root_uri}/default.asp");

    initialize_with_root(&mut stdin, &mut reader, &root_uri);
    fs::write(
        root.join("helpers.inc"),
        "<%\nFunction BuildName(first)\nBuildName = first\nEnd Function\n%>",
    )
    .expect("write include");
    write_message(
        &mut stdin,
        &json!({
            "jsonrpc": "2.0",
            "method": "workspace/didCreateFiles",
            "params": {
                "files": [{ "uri": include_uri }],
            },
        }),
    );
    write_message(
        &mut stdin,
        &json!({
            "jsonrpc": "2.0",
            "method": "textDocument/didOpen",
            "params": {
                "textDocument": {
                    "uri": page_uri,
                    "languageId": "classic-asp",
                    "version": 1,
                    "text": "<!-- #include file=\"helpers.inc\" -->\n<%\nResponse.Write BuildName(\"Ada\")\n%>",
                },
            },
        }),
    );
    read_until(&mut reader, |message| {
        message["method"] == json!("textDocument/publishDiagnostics")
    });

    let references = request(
        &mut stdin,
        &mut reader,
        30,
        "textDocument/references",
        json!({
            "textDocument": { "uri": page_uri },
            "position": { "line": 2, "character": 16 },
            "context": { "includeDeclaration": true },
        }),
    );
    let serialized = references.to_string();
    assert!(
        serialized.contains("helpers.inc"),
        "references after file operation: {serialized}"
    );

    shutdown(&mut stdin, &mut reader);
    drop(stdin);
    assert!(child.wait().expect("wait server").success());
    let _ = fs::remove_dir_all(root);
}

#[test]
fn execute_command_reindexes_workspace_files() {
    let root = std::env::temp_dir().join(format!(
        "asp-lsp-rust-command-reindex-{}",
        std::process::id()
    ));
    let _ = fs::remove_dir_all(&root);
    fs::create_dir_all(&root).expect("create temp root");

    let mut child = Command::new(env!("CARGO_BIN_EXE_asp-lsp-server"))
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .spawn()
        .expect("spawn server");

    let mut stdin = child.stdin.take().expect("server stdin");
    let stdout = child.stdout.take().expect("server stdout");
    let mut reader = BufReader::new(stdout);
    let root_uri = format!("file://{}", root.to_string_lossy());
    let page_uri = format!("{root_uri}/default.asp");

    initialize_with_root(&mut stdin, &mut reader, &root_uri);
    fs::write(
        root.join("helpers.inc"),
        "<%\nFunction BuildName(first)\nBuildName = first\nEnd Function\n%>",
    )
    .expect("write include");
    let reindex = request(
        &mut stdin,
        &mut reader,
        30,
        "workspace/executeCommand",
        json!({ "command": "aspLsp.server.reindexWorkspace" }),
    );
    assert_eq!(reindex["result"]["ok"], json!(true));

    write_message(
        &mut stdin,
        &json!({
            "jsonrpc": "2.0",
            "method": "textDocument/didOpen",
            "params": {
                "textDocument": {
                    "uri": page_uri,
                    "languageId": "classic-asp",
                    "version": 1,
                    "text": "<!-- #include file=\"helpers.inc\" -->\n<%\nResponse.Write BuildName(\"Ada\")\n%>",
                },
            },
        }),
    );
    read_until(&mut reader, |message| {
        message["method"] == json!("textDocument/publishDiagnostics")
    });
    let references = request(
        &mut stdin,
        &mut reader,
        31,
        "textDocument/references",
        json!({
            "textDocument": { "uri": page_uri },
            "position": { "line": 2, "character": 16 },
            "context": { "includeDeclaration": true },
        }),
    );
    assert!(references.to_string().contains("helpers.inc"));

    shutdown(&mut stdin, &mut reader);
    drop(stdin);
    assert!(child.wait().expect("wait server").success());
    let _ = fs::remove_dir_all(root);
}

#[test]
fn serves_workspace_diagnostics_with_disk_cache() {
    let root = std::env::temp_dir().join(format!(
        "asp-lsp-rust-workspace-diagnostics-{}",
        std::process::id()
    ));
    let _ = fs::remove_dir_all(&root);
    fs::create_dir_all(&root).expect("create temp root");
    fs::write(
        root.join("default.asp"),
        "<%\nOption Explicit\nmissingName = 1\n%>",
    )
    .expect("write asp");
    let cache_dir = root.join("cache");

    let mut child = Command::new(env!("CARGO_BIN_EXE_asp-lsp-server"))
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .spawn()
        .expect("spawn server");

    let mut stdin = child.stdin.take().expect("server stdin");
    let stdout = child.stdout.take().expect("server stdout");
    let mut reader = BufReader::new(stdout);
    let root_uri = format!("file://{}", root.to_string_lossy());

    initialize_with_settings_and_root(
        &mut stdin,
        &mut reader,
        json!({
            "cache": { "enabled": true, "directory": cache_dir.to_string_lossy() },
            "debug": { "output": "verbose" },
        }),
        &root_uri,
    );

    let first = request(
        &mut stdin,
        &mut reader,
        30,
        "workspace/diagnostic",
        json!({ "previousResultIds": [] }),
    );
    assert!(first["result"].to_string().contains("missingName"));
    assert!(
        fs::read_dir(&cache_dir)
            .expect("cache dir")
            .flatten()
            .any(|entry| entry.path().extension().and_then(|ext| ext.to_str()) == Some("cbor")),
        "expected cbor cache entry"
    );

    let clear_disk = request(
        &mut stdin,
        &mut reader,
        31,
        "workspace/executeCommand",
        json!({ "command": "aspLsp.server.clearDiskCache" }),
    );
    assert_eq!(clear_disk["result"]["ok"], json!(true));
    assert!(
        !fs::read_dir(&cache_dir)
            .expect("cache dir")
            .flatten()
            .any(|entry| entry.path().extension().and_then(|ext| ext.to_str()) == Some("cbor")),
        "expected clearDiskCache to remove cbor cache entries"
    );

    let second = request(
        &mut stdin,
        &mut reader,
        32,
        "workspace/diagnostic",
        json!({ "previousResultIds": [] }),
    );
    assert_eq!(second["result"], first["result"]);
    assert!(
        fs::read_dir(&cache_dir)
            .expect("cache dir")
            .flatten()
            .any(|entry| entry.path().extension().and_then(|ext| ext.to_str()) == Some("cbor")),
        "expected workspace diagnostics to repopulate disk cache"
    );

    let clear_process = request(
        &mut stdin,
        &mut reader,
        33,
        "workspace/executeCommand",
        json!({ "command": "aspLsp.server.clearProcessCache" }),
    );
    assert_eq!(clear_process["result"]["ok"], json!(true));
    let clear_all = request(
        &mut stdin,
        &mut reader,
        34,
        "workspace/executeCommand",
        json!({ "command": "aspLsp.server.clearCache" }),
    );
    assert_eq!(clear_all["result"]["ok"], json!(true));
    assert!(
        !fs::read_dir(&cache_dir)
            .expect("cache dir")
            .flatten()
            .any(|entry| entry.path().extension().and_then(|ext| ext.to_str()) == Some("cbor")),
        "expected clearCache to remove cbor cache entries"
    );

    shutdown(&mut stdin, &mut reader);
    drop(stdin);
    assert!(child.wait().expect("wait server").success());
    let _ = fs::remove_dir_all(root);
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
    let color = colors["result"]
        .as_array()
        .expect("embedded colors")
        .iter()
        .find(|color| color["range"]["start"]["line"] == json!(3))
        .expect("CSS color")
        .clone();

    let presentations = request(
        &mut stdin,
        &mut reader,
        25,
        "textDocument/colorPresentation",
        json!({
            "textDocument": { "uri": uri },
            "color": color["color"].clone(),
            "range": color["range"].clone(),
        }),
    );
    assert!(presentations["result"]
        .as_array()
        .expect("embedded color presentations")
        .iter()
        .any(|presentation| presentation["label"]
            .as_str()
            .is_some_and(|label| label.contains("rgb") || label.contains("#") || label == "red")));

    let linked = request(
        &mut stdin,
        &mut reader,
        26,
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

fn initialize_with_root(
    stdin: &mut std::process::ChildStdin,
    reader: &mut BufReader<std::process::ChildStdout>,
    root_uri: &str,
) {
    initialize_with_settings_and_root(stdin, reader, json!({}), root_uri);
}

fn initialize_with_settings(
    stdin: &mut std::process::ChildStdin,
    reader: &mut BufReader<std::process::ChildStdout>,
    settings: Value,
) {
    initialize_with_settings_and_root(stdin, reader, settings, "file:///tmp");
}

fn initialize_with_settings_and_root(
    stdin: &mut std::process::ChildStdin,
    reader: &mut BufReader<std::process::ChildStdout>,
    settings: Value,
    root_uri: &str,
) {
    write_message(
        stdin,
        &json!({
            "jsonrpc": "2.0",
            "id": 1,
            "method": "initialize",
            "params": {
                "processId": std::process::id(),
                "rootUri": root_uri,
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
    assert_eq!(
        initialize["result"]["capabilities"]["textDocumentSync"]["willSave"],
        json!(true)
    );
    assert_eq!(
        initialize["result"]["capabilities"]["textDocumentSync"]["willSaveWaitUntil"],
        json!(true)
    );
    assert_eq!(
        initialize["result"]["capabilities"]["textDocumentSync"]["save"]["includeText"],
        json!(true)
    );
    assert_eq!(
        initialize["result"]["capabilities"]["completionProvider"]["triggerCharacters"],
        json!(["<", ".", "\"", "'", ":", "#", "(", " "])
    );
    assert_eq!(
        initialize["result"]["capabilities"]["executeCommandProvider"]["commands"],
        json!([
            "aspLsp.server.reindexWorkspace",
            "aspLsp.server.clearCache",
            "aspLsp.server.clearDiskCache",
            "aspLsp.server.clearProcessCache",
        ])
    );
    assert_eq!(
        initialize["result"]["capabilities"]["workspace"]["fileOperations"]["didCreate"]["filters"]
            [0]["pattern"]["glob"],
        json!("**/*.{asp,asa,inc}")
    );
    assert_eq!(
        initialize["result"]["capabilities"]["workspace"]["fileOperations"]["didRename"]["filters"]
            [0]["pattern"]["glob"],
        json!("**/*.{asp,asa,inc}")
    );
    assert_eq!(
        initialize["result"]["capabilities"]["workspace"]["fileOperations"]["didDelete"]["filters"]
            [0]["pattern"]["glob"],
        json!("**/*.{asp,asa,inc}")
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

fn documentation_action_new_text(actions: &Value, uri: &str) -> String {
    actions["result"]
        .as_array()
        .expect("documentation actions")
        .iter()
        .find(|action| action["title"] == json!("Generate VBScript documentation"))
        .expect("documentation code action")["edit"]["changes"][uri][0]["newText"]
        .as_str()
        .expect("documentation new text")
        .to_string()
}

#[derive(Debug)]
struct DecodedSemanticToken {
    line: u64,
    character: u64,
    token_type: u64,
}

fn decode_semantic_tokens(data: &[Value]) -> Vec<DecodedSemanticToken> {
    let mut line = 0;
    let mut character = 0;
    let mut tokens = Vec::new();
    for chunk in data.chunks(5) {
        if chunk.len() < 5 {
            continue;
        }
        let delta_line = chunk[0].as_u64().expect("delta line");
        let delta_character = chunk[1].as_u64().expect("delta character");
        line += delta_line;
        character = if delta_line == 0 {
            character + delta_character
        } else {
            delta_character
        };
        tokens.push(DecodedSemanticToken {
            line,
            character,
            token_type: chunk[3].as_u64().expect("token type"),
        });
    }
    tokens
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
