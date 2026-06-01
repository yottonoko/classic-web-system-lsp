use std::collections::HashMap;
use std::time::{Duration, Instant};

use asp_ide::{Ide, TextPosition, TextRange};
use crossbeam_channel::RecvTimeoutError;
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
    let mut state = ServerState::default();
    state.set_settings(settings_from_initialize(&initialize_params))?;
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
    publish_backend_status(&connection, &state.ide)?;

    loop {
        match receive_message(&connection, state.next_diagnostics_timeout()) {
            Ok(message) => match message {
                Message::Request(request) => {
                    if handle_request(&connection, &mut state, request)? {
                        break;
                    }
                }
                Message::Notification(notification) => {
                    handle_notification(&connection, &mut state, notification)?;
                }
                Message::Response(_) => {}
            },
            Err(RecvTimeoutError::Timeout) => {}
            Err(RecvTimeoutError::Disconnected) => break,
        }
        state.publish_due_diagnostics(&connection)?;
    }

    drop(connection);
    io_threads.join().map_err(|error| error.to_string())
}

fn receive_message(
    connection: &Connection,
    timeout: Option<Duration>,
) -> Result<Message, RecvTimeoutError> {
    match timeout {
        Some(timeout) => connection.receiver.recv_timeout(timeout),
        None => connection
            .receiver
            .recv()
            .map_err(|_| RecvTimeoutError::Disconnected),
    }
}

#[derive(Default)]
struct ServerState {
    ide: Ide,
    diagnostics: DiagnosticScheduler,
}

impl ServerState {
    fn set_settings(&mut self, settings: Value) -> Result<Vec<(String, Vec<Value>)>, String> {
        self.diagnostics.set_debounce_from_settings(&settings);
        self.ide.set_settings(settings)
    }

    fn next_diagnostics_timeout(&self) -> Option<Duration> {
        self.diagnostics.next_timeout()
    }

    fn schedule_diagnostics(&mut self, uri: String) {
        self.diagnostics.schedule(uri);
    }

    fn clear_scheduled_diagnostics(&mut self, uri: &str) {
        self.diagnostics.clear(uri);
    }

    fn publish_due_diagnostics(&mut self, connection: &Connection) -> Result<(), String> {
        for uri in self.diagnostics.take_due() {
            let diagnostics = self.ide.diagnostics(&uri)?;
            send_diagnostics(connection, &uri, diagnostics)?;
        }
        Ok(())
    }

    fn publish_fast_diagnostics(&self, connection: &Connection, uri: &str) -> Result<(), String> {
        let diagnostics = self.ide.parser_diagnostics(uri)?;
        send_diagnostics(connection, uri, diagnostics)
    }
}

struct DiagnosticScheduler {
    debounce: Duration,
    pending: HashMap<String, Instant>,
}

impl Default for DiagnosticScheduler {
    fn default() -> Self {
        Self {
            debounce: Duration::from_millis(250),
            pending: HashMap::new(),
        }
    }
}

impl DiagnosticScheduler {
    fn set_debounce_from_settings(&mut self, settings: &Value) {
        if let Some(milliseconds) = settings
            .get("diagnostics")
            .and_then(|diagnostics| diagnostics.get("debounceMs"))
            .and_then(Value::as_u64)
        {
            self.debounce = Duration::from_millis(milliseconds);
        }
    }

    fn schedule(&mut self, uri: String) {
        self.pending.insert(uri, Instant::now() + self.debounce);
    }

    fn clear(&mut self, uri: &str) {
        self.pending.remove(uri);
    }

    fn next_timeout(&self) -> Option<Duration> {
        let now = Instant::now();
        self.pending
            .values()
            .min()
            .map(|deadline| deadline.saturating_duration_since(now))
    }

    fn take_due(&mut self) -> Vec<String> {
        let now = Instant::now();
        let due = self
            .pending
            .iter()
            .filter_map(|(uri, deadline)| (*deadline <= now).then(|| uri.clone()))
            .collect::<Vec<_>>();
        for uri in &due {
            self.pending.remove(uri);
        }
        due
    }
}

fn handle_request(
    connection: &Connection,
    state: &mut ServerState,
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
                .send(Response::new_ok(request.id, state.ide.backend_status()).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "workspace/executeCommand" => {
            connection
                .sender
                .send(Response::new_ok(request.id, json!({ "ok": true })).into())
                .map_err(|error| error.to_string())?;
            publish_backend_status(connection, &state.ide)?;
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
    state: &mut ServerState,
    notification: Notification,
) -> Result<(), String> {
    match notification.method.as_str() {
        "exit" => {}
        "textDocument/didOpen" => {
            let uri = pointer_string(&notification.params, "/textDocument/uri");
            let text = pointer_string(&notification.params, "/textDocument/text");
            open_document(connection, state, uri, text)?;
        }
        "textDocument/didChange" => {
            let uri = pointer_string(&notification.params, "/textDocument/uri");
            update_changed_document(connection, state, uri, &notification.params)?;
        }
        "textDocument/didClose" => {
            let uri = pointer_string(&notification.params, "/textDocument/uri");
            state.ide.close_document(&uri);
            state.clear_scheduled_diagnostics(&uri);
            send_diagnostics(connection, &uri, Vec::new())?;
        }
        "workspace/didChangeConfiguration" => {
            if let Some(settings) = notification.params.get("settings") {
                for (uri, diagnostics) in state.set_settings(
                    settings
                        .get("aspLsp")
                        .cloned()
                        .unwrap_or_else(|| settings.clone()),
                )? {
                    send_diagnostics(connection, &uri, diagnostics)?;
                }
            }
            publish_backend_status(connection, &state.ide)?;
        }
        _ => {}
    }
    Ok(())
}

fn update_changed_document(
    connection: &Connection,
    state: &mut ServerState,
    uri: String,
    params: &Value,
) -> Result<(), String> {
    let Some(changes) = params.get("contentChanges").and_then(Value::as_array) else {
        return Ok(());
    };
    for change in changes {
        let text = change
            .get("text")
            .and_then(Value::as_str)
            .unwrap_or_default()
            .to_string();
        if let Some(range) = change.get("range").and_then(text_range) {
            state.ide.edit_document_text(uri.clone(), range, text)?;
        } else {
            state.ide.replace_document_text(uri.clone(), text);
        }
    }
    state.publish_fast_diagnostics(connection, &uri)?;
    state.schedule_diagnostics(uri);
    Ok(())
}

fn open_document(
    connection: &Connection,
    state: &mut ServerState,
    uri: String,
    text: String,
) -> Result<(), String> {
    state.ide.replace_document_text(uri.clone(), text);
    state.publish_fast_diagnostics(connection, &uri)?;
    state.schedule_diagnostics(uri);
    Ok(())
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
