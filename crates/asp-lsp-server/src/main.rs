use asp_ide::{Ide, TextPosition, TextRange};
use lsp_server::{Connection, ErrorCode, Message, Notification, Request, Response};
use serde_json::{json, Value};

const BACKEND_STATUS_METHOD: &str = "aspLsp/backendStatus";

fn main() {
    if let Err(error) = run() {
        eprintln!("{error}");
        std::process::exit(1);
    }
}

fn run() -> Result<(), String> {
    let (connection, io_threads) = Connection::stdio();
    let (initialize_id, initialize_params) = connection
        .initialize_start()
        .map_err(|error| error.to_string())?;
    let mut ide = Ide::default();
    ide.set_settings(settings_from_initialize(&initialize_params))?;
    connection
        .initialize_finish(
            initialize_id,
            json!({
                "capabilities": server_capabilities(),
                "serverInfo": {
                    "name": "asp-lsp-server",
                    "version": env!("CARGO_PKG_VERSION"),
                },
            }),
        )
        .map_err(|error| error.to_string())?;
    publish_backend_status(&connection, &ide)?;

    for message in &connection.receiver {
        match message {
            Message::Request(request) => {
                if handle_request(&connection, &mut ide, request)? {
                    break;
                }
            }
            Message::Notification(notification) => {
                handle_notification(&connection, &mut ide, notification)?;
            }
            Message::Response(_) => {}
        }
    }

    drop(connection);
    io_threads.join().map_err(|error| error.to_string())
}

fn server_capabilities() -> Value {
    json!({
        "textDocumentSync": {
            "openClose": true,
            "change": 2,
            "save": { "includeText": true },
        },
        "executeCommandProvider": {
            "commands": [
                "aspLsp.server.reindexWorkspace",
                "aspLsp.server.clearCache",
                "aspLsp.server.clearDiskCache",
                "aspLsp.server.clearProcessCache",
            ],
        },
        "positionEncoding": "utf-16",
    })
}

fn settings_from_initialize(params: &Value) -> Value {
    params
        .get("initializationOptions")
        .and_then(|options| options.get("settings"))
        .cloned()
        .unwrap_or_else(|| json!({}))
}

fn handle_request(
    connection: &Connection,
    ide: &mut Ide,
    request: Request,
) -> Result<bool, String> {
    match request.method.as_str() {
        "shutdown" => {
            connection
                .sender
                .send(Response::new_ok(request.id, Value::Null).into())
                .map_err(|error| error.to_string())?;
            Ok(true)
        }
        BACKEND_STATUS_METHOD => {
            connection
                .sender
                .send(Response::new_ok(request.id, ide.backend_status()).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "workspace/executeCommand" => {
            connection
                .sender
                .send(Response::new_ok(request.id, json!({ "ok": true })).into())
                .map_err(|error| error.to_string())?;
            publish_backend_status(connection, ide)?;
            Ok(false)
        }
        _ => {
            connection
                .sender
                .send(
                    Response::new_err(
                        request.id,
                        ErrorCode::MethodNotFound as i32,
                        format!("method not implemented: {}", request.method),
                    )
                    .into(),
                )
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
    }
}

fn handle_notification(
    connection: &Connection,
    ide: &mut Ide,
    notification: Notification,
) -> Result<(), String> {
    match notification.method.as_str() {
        "exit" => {}
        "textDocument/didOpen" => {
            let uri = pointer_string(&notification.params, "/textDocument/uri");
            let text = pointer_string(&notification.params, "/textDocument/text");
            publish_document_diagnostics(connection, ide, uri, text)?;
        }
        "textDocument/didChange" => {
            let uri = pointer_string(&notification.params, "/textDocument/uri");
            publish_changed_document_diagnostics(connection, ide, uri, &notification.params)?;
        }
        "textDocument/didClose" => {
            let uri = pointer_string(&notification.params, "/textDocument/uri");
            ide.close_document(&uri);
            send_diagnostics(connection, &uri, Vec::new())?;
        }
        "workspace/didChangeConfiguration" => {
            if let Some(settings) = notification.params.get("settings") {
                for (uri, diagnostics) in ide.set_settings(
                    settings
                        .get("aspLsp")
                        .cloned()
                        .unwrap_or_else(|| settings.clone()),
                )? {
                    send_diagnostics(connection, &uri, diagnostics)?;
                }
            }
            publish_backend_status(connection, ide)?;
        }
        _ => {}
    }
    Ok(())
}

fn publish_changed_document_diagnostics(
    connection: &Connection,
    ide: &mut Ide,
    uri: String,
    params: &Value,
) -> Result<(), String> {
    let Some(changes) = params.get("contentChanges").and_then(Value::as_array) else {
        return Ok(());
    };
    let mut latest_diagnostics = None;
    for change in changes {
        let text = change
            .get("text")
            .and_then(Value::as_str)
            .unwrap_or_default()
            .to_string();
        latest_diagnostics = Some(
            if let Some(range) = change.get("range").and_then(text_range) {
                ide.change_document_incremental(uri.clone(), range, text)?
            } else {
                ide.change_document_full(uri.clone(), text)?
            },
        );
    }
    send_diagnostics(connection, &uri, latest_diagnostics.unwrap_or_default())
}

fn publish_document_diagnostics(
    connection: &Connection,
    ide: &mut Ide,
    uri: String,
    text: String,
) -> Result<(), String> {
    let diagnostics = ide.change_document_full(uri.clone(), text)?;
    send_diagnostics(connection, &uri, diagnostics)
}

fn text_range(value: &Value) -> Option<TextRange> {
    Some(TextRange {
        start: text_position(value.get("start")?)?,
        end: text_position(value.get("end")?)?,
    })
}

fn text_position(value: &Value) -> Option<TextPosition> {
    Some(TextPosition {
        line: value.get("line")?.as_u64()?.try_into().ok()?,
        character: value.get("character")?.as_u64()?.try_into().ok()?,
    })
}

fn send_diagnostics(
    connection: &Connection,
    uri: &str,
    diagnostics: Vec<Value>,
) -> Result<(), String> {
    connection
        .sender
        .send(
            Notification::new(
                "textDocument/publishDiagnostics".to_string(),
                json!({ "uri": uri, "diagnostics": diagnostics }),
            )
            .into(),
        )
        .map_err(|error| error.to_string())
}

fn publish_backend_status(connection: &Connection, ide: &Ide) -> Result<(), String> {
    connection
        .sender
        .send(Notification::new(BACKEND_STATUS_METHOD.to_string(), ide.backend_status()).into())
        .map_err(|error| error.to_string())
}

fn pointer_string(params: &Value, pointer: &str) -> String {
    params
        .pointer(pointer)
        .and_then(Value::as_str)
        .unwrap_or_default()
        .to_string()
}
