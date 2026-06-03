// serde_json::json! builds large nested LSP capability objects in this file.
#![recursion_limit = "256"]

use std::collections::HashMap;
use std::env;
use std::fs;
use std::io::{Read, Write};
use std::path::{Path, PathBuf};
use std::process::{Child, ChildStdin, ChildStdout, Command, Stdio};
use std::thread;
use std::time::{Duration, Instant, UNIX_EPOCH};

use asp_ide::{Ide, IncludeImpact, MappedVirtualDocument, TextPosition, TextRange};
use asp_sidecar_protocol::{EmbeddedRequest, EmbeddedResponse, VirtualDocument};
use crossbeam_channel::RecvTimeoutError;
use lsp_server::{Connection, ErrorCode, Message, Notification, Request, Response};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};

const BACKEND_STATUS_METHOD: &str = "aspLsp/backendStatus";
const RA_VIEW_FILE_TEXT_METHOD: &str = "rust-analyzer/viewFileText";
const RA_VIEW_SYNTAX_TREE_METHOD: &str = "rust-analyzer/viewSyntaxTree";
const RA_ANALYZER_STATUS_METHOD: &str = "rust-analyzer/analyzerStatus";
const RA_MEMORY_USAGE_METHOD: &str = "rust-analyzer/memoryUsage";
const RA_OPEN_SERVER_LOGS_METHOD: &str = "rust-analyzer/openServerLogs";
const RA_MATCHING_BRACE_METHOD: &str = "rust-analyzer/matchingBrace";
const EXPERIMENTAL_PARENT_MODULE_METHOD: &str = "experimental/parentModule";
const EXPERIMENTAL_CHILD_MODULES_METHOD: &str = "experimental/childModules";
const EXPERIMENTAL_JOIN_LINES_METHOD: &str = "experimental/joinLines";
const EXPERIMENTAL_ON_ENTER_METHOD: &str = "experimental/onEnter";
const EXPERIMENTAL_MOVE_ITEM_METHOD: &str = "experimental/moveItem";
const EXPERIMENTAL_EXTERNAL_DOCS_METHOD: &str = "experimental/externalDocs";
const EXPERIMENTAL_SSR_METHOD: &str = "experimental/ssr";
const FRAME_KIND_JSON: u8 = 1;
const VSCODE_PACKAGE_JSON: &str = include_str!("../../../apps/vscode/package.json");

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
    state.set_workspace_roots(workspace_roots_from_initialize(&initialize_params))?;
    connection
        .initialize_finish(
            initialize_id,
            json!({
                "capabilities": server_capabilities(),
                "serverInfo": {
                    "name": "asp-lsp-server",
                    "version": language_server_version(),
                },
            }),
        )
        .map_err(|error| error.to_string())?;
    state.log_workspace_index_completed(&connection, "workspaceRoots")?;
    publish_backend_status(&connection, &state)?;

    loop {
        match receive_message(&connection, state.next_server_timeout()) {
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
    sidecar: EmbeddedSidecar,
    semantic_tokens: SemanticTokenCache,
    indexed_files: Vec<IndexedFile>,
    settings: Value,
    workspace_roots: Vec<String>,
    sidecar_request_id: u64,
    sidecar_project_generation: u64,
    sidecar_project_fingerprint: String,
    sidecar_project_reset_reason: String,
    sidecar_forced_reset_generation: u64,
}

impl ServerState {
    fn set_settings(&mut self, settings: Value) -> Result<Vec<(String, Vec<Value>)>, String> {
        let previous_max_index_files = workspace_max_index_files(&self.settings);
        let next_max_index_files = workspace_max_index_files(&settings);
        self.diagnostics.set_debounce_from_settings(&settings);
        self.settings = settings.clone();
        self.bump_sidecar_project_generation("settings");
        if previous_max_index_files != next_max_index_files && !self.workspace_roots.is_empty() {
            self.refresh_workspace_index()?;
        }
        self.ide.set_settings(settings)
    }

    fn set_workspace_roots(&mut self, roots: Vec<String>) -> Result<(), String> {
        self.set_workspace_roots_with_reason(roots, "workspaceRoots")
    }

    fn set_workspace_roots_with_reason(
        &mut self,
        roots: Vec<String>,
        reason: &str,
    ) -> Result<(), String> {
        let indexed_files =
            index_workspace_files(&roots, workspace_max_index_files(&self.settings))?;
        self.ide.replace_indexed_documents(
            indexed_files
                .iter()
                .map(|file| (file.uri.clone(), file.text.clone()))
                .collect(),
        );
        self.indexed_files = indexed_files;
        self.workspace_roots = roots;
        self.bump_sidecar_project_generation(reason);
        Ok(())
    }

    fn refresh_workspace_index(&mut self) -> Result<(), String> {
        self.set_workspace_roots_with_reason(self.workspace_roots.clone(), "workspaceIndex")
    }

    fn log_workspace_index_completed(
        &self,
        connection: &Connection,
        reason: &str,
    ) -> Result<(), String> {
        log_debug_only(
            connection,
            &self.settings,
            format!(
                "[asp-lsp] workspaceIndex.completed: reason={reason}, roots={}, files={}, maxFiles={}",
                self.workspace_roots.len(),
                self.indexed_files.len(),
                workspace_max_index_files(&self.settings)
            ),
        )
    }

    fn execute_command(&mut self, command: &str) -> Result<Value, String> {
        match command {
            "aspLsp.server.reindexWorkspace" => {
                self.refresh_workspace_index()?;
            }
            "aspLsp.server.clearCache" => {
                self.ide.clear_process_cache();
                self.semantic_tokens.clear_all();
                self.bump_sidecar_project_generation("clearCache");
            }
            "aspLsp.server.clearProcessCache" => {
                self.ide.clear_process_cache();
                self.semantic_tokens.clear_all();
                self.bump_sidecar_project_generation("clearProcessCache");
            }
            _ => {
                return Err(format!("unknown command: {command}"));
            }
        }
        Ok(json!({ "ok": true, "command": command }))
    }

    fn next_server_timeout(&self) -> Option<Duration> {
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
            let started_at = Instant::now();
            log_debug_only(
                connection,
                &self.settings,
                format!("[asp-lsp] diagnostics.start: uri={uri}"),
            )?;
            let diagnostics = self.full_diagnostics(Some(connection), &uri)?;
            let diagnostic_count = diagnostics.len();
            send_diagnostics(connection, &uri, diagnostics)?;
            self.log_check_completed(connection, &uri, started_at, diagnostic_count)?;
        }
        Ok(())
    }

    fn backend_status(&self) -> Value {
        let mut status = self.ide.backend_status();
        if let Some(object) = status.as_object_mut() {
            object.insert(
                "sidecar".to_string(),
                json!({
                    "status": if self.sidecar.is_running() { "running" } else { "not-started" },
                }),
            );
        }
        status
    }

    fn publish_fast_diagnostics(&self, connection: &Connection, uri: &str) -> Result<(), String> {
        let diagnostics = self.ide.parser_diagnostics(uri)?;
        send_diagnostics(connection, uri, diagnostics)
    }

    fn full_diagnostics(
        &mut self,
        connection: Option<&Connection>,
        uri: &str,
    ) -> Result<Vec<Value>, String> {
        let mut diagnostics = self.ide.diagnostics(uri)?;
        diagnostics.extend(self.embedded_diagnostics(connection, uri)?);
        Ok(diagnostics)
    }

    fn text_document_diagnostic(&mut self, uri: &str) -> Result<Value, String> {
        let diagnostics = self.full_diagnostics(None, uri)?;
        Ok(json!({
            "kind": "full",
            "items": diagnostics,
        }))
    }

    fn semantic_tokens_full(&mut self, uri: &str) -> Result<Value, String> {
        let fingerprint = self.semantic_tokens_fingerprint(uri);
        if let Some(value) = self
            .semantic_tokens
            .full_from_cached(uri, fingerprint.as_deref())
        {
            return Ok(value);
        }
        let value = self.semantic_tokens_value(None, uri)?;
        Ok(self.semantic_tokens.full(uri, value, fingerprint))
    }

    fn semantic_tokens_range(&mut self, uri: &str, range: TextRange) -> Result<Value, String> {
        let fingerprint = self.semantic_tokens_fingerprint(uri);
        if let Some(value) =
            self.semantic_tokens
                .range_from_cached(uri, fingerprint.as_deref(), &range)
        {
            return Ok(value);
        }
        self.semantic_tokens_value(Some(range), uri)
    }

    fn semantic_tokens_delta(
        &mut self,
        uri: &str,
        previous_result_id: &str,
    ) -> Result<Value, String> {
        let fingerprint = self.semantic_tokens_fingerprint(uri);
        if let Some(value) =
            self.semantic_tokens
                .delta_from_cached(uri, previous_result_id, fingerprint.as_deref())
        {
            return Ok(value);
        }
        let value = self.semantic_tokens_value(None, uri)?;
        Ok(self
            .semantic_tokens
            .delta(uri, previous_result_id, value, fingerprint))
    }

    fn semantic_tokens_fingerprint(&self, uri: &str) -> Option<String> {
        let ide = self.ide.semantic_tokens_fingerprint(uri)?;
        Some(stable_hash(&format!(
            "{ide}\0{}",
            self.sidecar_project_fingerprint
        )))
    }

    fn semantic_tokens_value(
        &mut self,
        range: Option<TextRange>,
        uri: &str,
    ) -> Result<Value, String> {
        let virtuals = self.ide.embedded_virtual_documents(uri)?;
        let rust_tokens = thread::scope(|scope| {
            let embedded_handle =
                scope.spawn(move || collect_fast_embedded_semantic_tokens(virtuals, range));
            let rust_tokens = self.ide.semantic_tokens(uri, range);
            let embedded_tokens = embedded_handle.join().unwrap_or_default();
            (rust_tokens, embedded_tokens)
        });
        let (value, embedded_tokens) = rust_tokens;
        let mut value = value?;
        let Some(data) = value.get("data").and_then(Value::as_array) else {
            return Ok(value);
        };
        let Some(mut tokens) = decode_semantic_tokens(data) else {
            return Ok(value);
        };
        tokens.extend(embedded_tokens);
        tokens.sort_by_key(|token| (token.line, token.character));
        tokens.dedup_by(|left, right| {
            left.line == right.line
                && left.character == right.character
                && left.length == right.length
                && left.token_type == right.token_type
                && left.token_modifiers == right.token_modifiers
        });
        if let Some(object) = value.as_object_mut() {
            object.insert(
                "data".to_string(),
                Value::Array(encode_decoded_semantic_tokens(&tokens)),
            );
        }
        Ok(value)
    }

    fn clear_semantic_tokens(&mut self, uri: &str) {
        self.semantic_tokens.clear_uri(uri);
    }

    fn log_check_completed(
        &self,
        connection: &Connection,
        uri: &str,
        started_at: Instant,
        diagnostic_count: usize,
    ) -> Result<(), String> {
        log_debug_summary(
            connection,
            &self.settings,
            format!(
                "[asp-lsp] LSP check completed: {uri} {:.2}ms, diagnostics={diagnostic_count}",
                started_at.elapsed().as_secs_f64() * 1000.0
            ),
        )
    }

    fn embedded_diagnostics(
        &mut self,
        connection: Option<&Connection>,
        uri: &str,
    ) -> Result<Vec<Value>, String> {
        collect_embedded_diagnostics(
            &mut self.sidecar,
            &self.ide,
            connection,
            &self.settings,
            &self.workspace_roots,
            self.sidecar_project_generation,
            &self.sidecar_project_fingerprint,
            &self.sidecar_project_reset_reason,
            &mut self.sidecar_request_id,
            uri,
            embedded_parallelism(&self.settings),
        )
    }

    fn embedded_position_feature(
        &mut self,
        connection: &Connection,
        uri: &str,
        operation: &str,
        position: TextPosition,
    ) -> Result<Option<Value>, String> {
        self.embedded_position_feature_with_params(connection, uri, operation, position, json!({}))
    }

    fn embedded_position_feature_with_params(
        &mut self,
        connection: &Connection,
        uri: &str,
        operation: &str,
        position: TextPosition,
        mut params: Value,
    ) -> Result<Option<Value>, String> {
        let Some((mapped, virtual_position)) =
            self.ide.embedded_virtual_document_at(uri, position)?
        else {
            return Ok(None);
        };
        if mapped.document.language_id == "vbscript" {
            return Ok(None);
        }
        let open_virtuals = self.open_virtual_documents(uri)?;
        if let Some(object) = params.as_object_mut() {
            object.insert("position".to_string(), virtual_position);
        }
        let result =
            self.embedded_request(Some(connection), &mapped, open_virtuals, operation, params)?;
        Ok(result.map(|value| mapped.remap_lsp_value(value)))
    }

    fn embedded_range_feature(
        &mut self,
        connection: &Connection,
        uri: &str,
        operation: &str,
        range: TextRange,
        mut params: Value,
    ) -> Result<Option<Value>, String> {
        let virtuals = self.ide.embedded_virtual_documents(uri)?;
        let open_virtuals = virtuals
            .iter()
            .map(|mapped| mapped.document.clone())
            .collect::<Vec<_>>();
        for mapped in virtuals {
            if mapped.document.language_id == "vbscript" {
                continue;
            }
            let Some(virtual_range) = mapped.virtual_range_for_source_range(range) else {
                continue;
            };
            if let Some(object) = params.as_object_mut() {
                object.insert("range".to_string(), virtual_range);
            }
            let Some(result) = self.embedded_request(
                Some(connection),
                &mapped,
                open_virtuals.clone(),
                operation,
                params.clone(),
            )?
            else {
                continue;
            };
            return Ok(Some(mapped.remap_lsp_value(result)));
        }
        Ok(None)
    }

    fn embedded_document_highlights(
        &mut self,
        connection: &Connection,
        uri: &str,
        position: TextPosition,
    ) -> Result<Option<Value>, String> {
        self.embedded_position_feature(connection, uri, "documentHighlights", position)
    }

    fn embedded_selection_ranges(
        &mut self,
        connection: &Connection,
        uri: &str,
        positions: &[TextPosition],
    ) -> Result<Option<Value>, String> {
        let mut result = self.ide.selection_ranges(uri, positions)?;
        let Some(items) = result.as_array_mut() else {
            return Ok(Some(result));
        };
        for (index, position) in positions.iter().copied().enumerate() {
            let Some((mapped, virtual_position)) =
                self.ide.embedded_virtual_document_at(uri, position)?
            else {
                continue;
            };
            if mapped.document.language_id == "vbscript" {
                continue;
            }
            let open_virtuals = self.open_virtual_documents(uri)?;
            let Some(value) = self.embedded_request(
                Some(connection),
                &mapped,
                open_virtuals,
                "selectionRanges",
                json!({ "positions": [virtual_position] }),
            )?
            else {
                continue;
            };
            let value = mapped.remap_lsp_value(value);
            let Some(selection) = value.as_array().and_then(|values| values.first()).cloned()
            else {
                continue;
            };
            if let Some(item) = items.get_mut(index) {
                *item = selection;
            }
        }
        Ok(Some(result))
    }

    fn embedded_prepare_rename(
        &mut self,
        connection: &Connection,
        uri: &str,
        position: TextPosition,
    ) -> Result<Option<Value>, String> {
        self.embedded_position_feature(connection, uri, "prepareRename", position)
    }

    fn embedded_rename(
        &mut self,
        connection: &Connection,
        uri: &str,
        position: TextPosition,
        new_name: &str,
    ) -> Result<Option<Value>, String> {
        let Some((mapped, virtual_position)) =
            self.ide.embedded_virtual_document_at(uri, position)?
        else {
            return Ok(None);
        };
        if mapped.document.language_id == "vbscript" {
            return Ok(None);
        }
        let open_virtuals = self.open_virtual_documents(uri)?;
        let Some(result) = self.embedded_request(
            Some(connection),
            &mapped,
            open_virtuals,
            "rename",
            json!({
                "position": virtual_position,
                "newName": new_name,
            }),
        )?
        else {
            return Ok(None);
        };
        Ok(Some(remap_workspace_edit(uri, &mapped, result)))
    }

    fn embedded_range_formatting(
        &mut self,
        connection: &Connection,
        uri: &str,
        range: TextRange,
        options: &Value,
    ) -> Result<Option<Value>, String> {
        self.embedded_range_feature(
            connection,
            uri,
            "rangeFormatting",
            range,
            json!({ "options": options }),
        )
    }

    fn embedded_on_type_formatting(
        &mut self,
        connection: &Connection,
        uri: &str,
        position: TextPosition,
        character: &str,
        options: &Value,
    ) -> Result<Option<Value>, String> {
        self.embedded_position_feature_with_params(
            connection,
            uri,
            "onTypeFormatting",
            position,
            json!({
                "character": character,
                "options": options,
            }),
        )
    }

    fn document_formatting(&mut self, uri: &str, options: &Value) -> Result<Value, String> {
        let Some(original) = self.ide.document_text(uri) else {
            return Ok(Value::Array(Vec::new()));
        };
        let replacement = self
            .ide
            .formatting_replacement(uri, None, options, &self.settings)?;
        let mut formatted = replacement
            .as_ref()
            .and_then(|value| value.get("newText"))
            .and_then(Value::as_str)
            .map(str::to_string)
            .unwrap_or_else(|| original.clone());
        let virtuals = self
            .ide
            .embedded_virtual_documents_for_text(uri, &formatted)?;
        let open_virtuals = virtuals
            .iter()
            .map(|mapped| mapped.document.clone())
            .collect::<Vec<_>>();
        let mut embedded_edits = Vec::new();
        for mapped in virtuals {
            if mapped.document.language_id == "vbscript" {
                continue;
            }
            let Some(result) = self.embedded_request(
                None,
                &mapped,
                open_virtuals.clone(),
                "formatting",
                json!({ "options": options }),
            )?
            else {
                continue;
            };
            if let Value::Array(values) = mapped.remap_lsp_value(result) {
                embedded_edits.extend(values.into_iter().filter(|value| !value.is_null()));
            }
        }
        if !embedded_edits.is_empty() {
            formatted = apply_lsp_text_edits(&formatted, &embedded_edits)?;
        }
        if formatted == original {
            return Ok(Value::Array(Vec::new()));
        }
        Ok(json!([{
            "range": full_text_range(&original),
            "newText": formatted,
        }]))
    }

    fn embedded_document_feature(
        &mut self,
        connection: &Connection,
        uri: &str,
        operation: &str,
    ) -> Result<Value, String> {
        let virtuals = self.ide.embedded_virtual_documents(uri)?;
        let open_virtuals = virtuals
            .iter()
            .map(|mapped| mapped.document.clone())
            .collect::<Vec<_>>();
        let mut items = Vec::new();
        for mapped in virtuals {
            if mapped.document.language_id == "vbscript" {
                continue;
            }
            let Some(result) = self.embedded_request(
                Some(connection),
                &mapped,
                open_virtuals.clone(),
                operation,
                Value::Null,
            )?
            else {
                continue;
            };
            match mapped.remap_lsp_value(result) {
                Value::Array(values) => items.extend(values),
                Value::Null => {}
                value => items.push(value),
            }
        }
        Ok(Value::Array(items))
    }

    fn embedded_color_presentations(
        &mut self,
        connection: &Connection,
        uri: &str,
        color: Value,
        range: TextRange,
    ) -> Result<Value, String> {
        let virtuals = self.ide.embedded_virtual_documents(uri)?;
        let open_virtuals = virtuals
            .iter()
            .map(|mapped| mapped.document.clone())
            .collect::<Vec<_>>();
        for mapped in virtuals {
            if mapped.document.language_id != "css" {
                continue;
            }
            let Some(virtual_range) = mapped.virtual_range_for_source_range(range) else {
                continue;
            };
            let Some(result) = self.embedded_request(
                Some(connection),
                &mapped,
                open_virtuals.clone(),
                "colorPresentations",
                json!({
                    "color": color,
                    "range": virtual_range,
                }),
            )?
            else {
                continue;
            };
            return Ok(mapped.remap_lsp_value(result));
        }
        Ok(Value::Array(Vec::new()))
    }

    fn embedded_request(
        &mut self,
        connection: Option<&Connection>,
        mapped: &MappedVirtualDocument,
        open_virtuals: Vec<VirtualDocument>,
        operation: &str,
        params: Value,
    ) -> Result<Option<Value>, String> {
        let request_id = self.next_sidecar_request_id();
        let open_virtual_count = open_virtuals.len();
        if let Some(connection) = connection {
            log_debug_only(
                connection,
                &self.settings,
                format!(
                    "[asp-lsp] sidecar.request: operation={operation}, language={}, openVirtuals={open_virtual_count}",
                    mapped.document.language_id
                ),
            )?;
        }
        let response = match self.sidecar.request(EmbeddedRequest {
            id: request_id,
            operation: operation.to_string(),
            active_virtual: mapped.document.clone(),
            open_virtuals,
            settings: self.settings.clone(),
            workspace_roots: self.workspace_roots.clone(),
            project_generation: self.sidecar_project_generation,
            project_fingerprint: Some(self.sidecar_project_fingerprint.clone()),
            project_reset_reason: Some(self.sidecar_project_reset_reason.clone()),
            params,
        }) {
            Ok(response) => response,
            Err(error) => {
                if !error.contains("embedded sidecar dist/sidecar.js was not found") {
                    eprintln!("embedded sidecar {operation} failed: {error}");
                }
                return Ok(None);
            }
        };
        log_sidecar_cache_stats(
            connection,
            &self.settings,
            operation,
            &mapped.document.language_id,
            response.cache_stats.as_ref(),
        )?;
        Ok(response.result)
    }

    fn open_virtual_documents(&self, uri: &str) -> Result<Vec<VirtualDocument>, String> {
        Ok(self
            .ide
            .embedded_virtual_documents(uri)?
            .into_iter()
            .map(|mapped| mapped.document)
            .collect())
    }

    fn next_sidecar_request_id(&mut self) -> u64 {
        self.sidecar_request_id += 1;
        self.sidecar_request_id
    }

    fn bump_sidecar_project_generation(&mut self, reason: &str) {
        self.sidecar_project_generation = self.sidecar_project_generation.wrapping_add(1);
        if matches!(reason, "clearCache" | "clearProcessCache") {
            self.sidecar_forced_reset_generation =
                self.sidecar_forced_reset_generation.wrapping_add(1);
        }
        self.sidecar_project_reset_reason = reason.to_string();
        self.sidecar_project_fingerprint = self.compute_sidecar_project_fingerprint();
    }

    fn compute_sidecar_project_fingerprint(&self) -> String {
        let mut roots = self.workspace_roots.clone();
        roots.sort();
        let mut indexed_files = self
            .indexed_files
            .iter()
            .map(|file| {
                json!({
                    "uri": &file.uri,
                    "fileName": &file.source.file_name,
                    "mtimeMs": file.source.mtime_ms,
                    "size": file.source.size,
                })
            })
            .collect::<Vec<_>>();
        indexed_files.sort_by_key(|value| value["fileName"].as_str().unwrap_or("").to_string());
        let mut project_files = sidecar_project_files(&roots);
        project_files.sort_by(|left, right| left.file_name.cmp(&right.file_name));
        stable_hash(
            &json!({
                "roots": roots,
                "settings": sidecar_project_settings_key(&self.settings),
                "indexedFiles": indexed_files,
                "projectFiles": project_files,
                "forcedResetGeneration": self.sidecar_forced_reset_generation,
            })
            .to_string(),
        )
    }
}

#[derive(Clone)]
struct IndexedFile {
    uri: String,
    text: String,
    source: DiskSourceMetadata,
}

#[derive(Clone, Deserialize, Serialize)]
struct DiskSourceMetadata {
    #[serde(rename = "fileName")]
    file_name: String,
    #[serde(rename = "mtimeMs")]
    mtime_ms: f64,
    size: u64,
}

#[derive(Default)]
struct SemanticTokenCache {
    next_id: u64,
    latest_by_uri: HashMap<String, String>,
    results: HashMap<String, SemanticTokenResult>,
}

struct SemanticTokenResult {
    uri: String,
    data: Vec<Value>,
    fingerprint: Option<String>,
}

impl SemanticTokenCache {
    fn full_from_cached(&mut self, uri: &str, fingerprint: Option<&str>) -> Option<Value> {
        let data = self.cached_latest_data(uri, fingerprint)?.to_vec();
        let fingerprint = fingerprint.map(str::to_string);
        let result_id = self.store(uri, data.clone(), fingerprint);
        Some(json!({
            "data": data,
            "resultId": result_id,
        }))
    }

    fn full(&mut self, uri: &str, mut value: Value, fingerprint: Option<String>) -> Value {
        let data = semantic_token_data(&value);
        let result_id = self.store(uri, data, fingerprint);
        if let Some(object) = value.as_object_mut() {
            object.insert("resultId".to_string(), Value::String(result_id));
        }
        value
    }

    fn delta(
        &mut self,
        uri: &str,
        previous_result_id: &str,
        value: Value,
        fingerprint: Option<String>,
    ) -> Value {
        let next = semantic_token_data(&value);
        let previous = self
            .results
            .get(previous_result_id)
            .filter(|result| result.uri == uri)
            .map(|result| result.data.as_slice());
        let edit = previous
            .map(|previous| semantic_token_delta_edit(previous, &next))
            .unwrap_or_else(|| json!({ "start": 0, "deleteCount": 0, "data": next.clone() }));
        let result_id = self.store(uri, next, fingerprint);
        json!({
            "resultId": result_id,
            "edits": [edit],
        })
    }

    fn delta_from_cached(
        &mut self,
        uri: &str,
        previous_result_id: &str,
        fingerprint: Option<&str>,
    ) -> Option<Value> {
        let previous = self
            .results
            .get(previous_result_id)
            .filter(|result| result.uri == uri)
            .filter(|result| result.fingerprint.as_deref() == fingerprint)?;
        let previous_data = previous.data.clone();
        let fingerprint = previous.fingerprint.clone();
        let edit = semantic_token_delta_edit(&previous_data, &previous_data);
        let result_id = self.store(uri, previous_data, fingerprint);
        Some(json!({
            "resultId": result_id,
            "edits": [edit],
        }))
    }

    fn range_from_cached(
        &self,
        uri: &str,
        fingerprint: Option<&str>,
        range: &TextRange,
    ) -> Option<Value> {
        let data = self.cached_latest_data(uri, fingerprint)?;
        let range_data = semantic_token_range_data(data, range)?;
        Some(json!({ "data": range_data }))
    }

    fn clear_uri(&mut self, uri: &str) {
        if let Some(result_id) = self.latest_by_uri.remove(uri) {
            self.results.remove(&result_id);
        }
    }

    fn clear_all(&mut self) {
        self.latest_by_uri.clear();
        self.results.clear();
    }

    fn store(&mut self, uri: &str, data: Vec<Value>, fingerprint: Option<String>) -> String {
        if let Some(previous) = self.latest_by_uri.get(uri) {
            self.results.remove(previous);
        }
        self.next_id += 1;
        let result_id = self.next_id.to_string();
        self.latest_by_uri
            .insert(uri.to_string(), result_id.clone());
        self.results.insert(
            result_id.clone(),
            SemanticTokenResult {
                uri: uri.to_string(),
                data,
                fingerprint,
            },
        );
        result_id
    }

    fn cached_latest_data(&self, uri: &str, fingerprint: Option<&str>) -> Option<&[Value]> {
        let result_id = self.latest_by_uri.get(uri)?;
        let result = self
            .results
            .get(result_id)
            .filter(|result| result.uri == uri)
            .filter(|result| result.fingerprint.as_deref() == fingerprint)?;
        Some(&result.data)
    }
}

fn semantic_token_data(value: &Value) -> Vec<Value> {
    value
        .get("data")
        .and_then(Value::as_array)
        .cloned()
        .unwrap_or_default()
}

fn semantic_token_delta_edit(previous: &[Value], next: &[Value]) -> Value {
    let mut prefix = 0;
    while prefix < previous.len() && prefix < next.len() && previous[prefix] == next[prefix] {
        prefix += 1;
    }
    let mut previous_suffix = previous.len();
    let mut next_suffix = next.len();
    while previous_suffix > prefix
        && next_suffix > prefix
        && previous[previous_suffix - 1] == next[next_suffix - 1]
    {
        previous_suffix -= 1;
        next_suffix -= 1;
    }
    json!({
        "start": prefix,
        "deleteCount": previous_suffix - prefix,
        "data": next[prefix..next_suffix].to_vec(),
    })
}

#[derive(Clone)]
struct DecodedSemanticToken {
    line: u64,
    character: u64,
    length: u64,
    token_type: u64,
    token_modifiers: u64,
}

fn semantic_token_range_data(data: &[Value], range: &TextRange) -> Option<Vec<Value>> {
    let start_line = u64::from(range.start.line);
    let start_character = u64::from(range.start.character);
    let end_line = u64::from(range.end.line);
    let end_character = u64::from(range.end.character);
    let tokens = decode_semantic_tokens(data)?;
    Some(encode_decoded_semantic_tokens(
        &tokens
            .into_iter()
            .filter(|token| {
                semantic_token_starts_in_range(
                    token,
                    start_line,
                    start_character,
                    end_line,
                    end_character,
                )
            })
            .collect::<Vec<_>>(),
    ))
}

fn decode_semantic_tokens(data: &[Value]) -> Option<Vec<DecodedSemanticToken>> {
    let mut line = 0_u64;
    let mut character = 0_u64;
    let mut tokens = Vec::new();
    for chunk in data.chunks_exact(5) {
        let delta_line = chunk[0].as_u64()?;
        let delta_start = chunk[1].as_u64()?;
        line = line.checked_add(delta_line)?;
        character = if delta_line == 0 {
            character.checked_add(delta_start)?
        } else {
            delta_start
        };
        tokens.push(DecodedSemanticToken {
            line,
            character,
            length: chunk[2].as_u64()?,
            token_type: chunk[3].as_u64()?,
            token_modifiers: chunk[4].as_u64()?,
        });
    }
    if data.len() % 5 == 0 {
        Some(tokens)
    } else {
        None
    }
}

fn encode_decoded_semantic_tokens(tokens: &[DecodedSemanticToken]) -> Vec<Value> {
    let mut data = Vec::with_capacity(tokens.len() * 5);
    let mut previous_line = 0_u64;
    let mut previous_character = 0_u64;
    for token in tokens {
        let delta_line = token.line.saturating_sub(previous_line);
        let delta_start = if delta_line == 0 {
            token.character.saturating_sub(previous_character)
        } else {
            token.character
        };
        data.push(Value::from(delta_line));
        data.push(Value::from(delta_start));
        data.push(Value::from(token.length));
        data.push(Value::from(token.token_type));
        data.push(Value::from(token.token_modifiers));
        previous_line = token.line;
        previous_character = token.character;
    }
    data
}

fn semantic_token_starts_in_range(
    token: &DecodedSemanticToken,
    start_line: u64,
    start_character: u64,
    end_line: u64,
    end_character: u64,
) -> bool {
    if token.line < start_line || token.line > end_line {
        return false;
    }
    if token.line == start_line && token.character < start_character {
        return false;
    }
    if token.line == end_line && token.character >= end_character {
        return false;
    }
    true
}

fn collect_embedded_diagnostics(
    sidecar: &mut EmbeddedSidecar,
    ide: &Ide,
    connection: Option<&Connection>,
    settings: &Value,
    workspace_roots: &[String],
    project_generation: u64,
    project_fingerprint: &str,
    project_reset_reason: &str,
    request_id: &mut u64,
    uri: &str,
    parallelism: usize,
) -> Result<Vec<Value>, String> {
    let virtuals = ide.embedded_virtual_documents(uri)?;
    collect_embedded_diagnostics_from_virtuals(
        sidecar,
        connection,
        settings,
        workspace_roots,
        project_generation,
        project_fingerprint,
        project_reset_reason,
        request_id,
        uri,
        virtuals,
        parallelism,
    )
}

fn collect_embedded_diagnostics_from_virtuals(
    sidecar: &mut EmbeddedSidecar,
    connection: Option<&Connection>,
    settings: &Value,
    workspace_roots: &[String],
    project_generation: u64,
    project_fingerprint: &str,
    project_reset_reason: &str,
    request_id: &mut u64,
    uri: &str,
    virtuals: Vec<MappedVirtualDocument>,
    parallelism: usize,
) -> Result<Vec<Value>, String> {
    let virtual_count = virtuals.len();
    let open_virtuals = virtuals
        .iter()
        .map(|mapped| mapped.document.clone())
        .collect::<Vec<_>>();
    let mut mapped_requests = Vec::new();
    let mut requests = Vec::new();
    for mapped in virtuals {
        if mapped.document.language_id == "vbscript" {
            continue;
        }
        *request_id = request_id.wrapping_add(1);
        requests.push(EmbeddedRequest {
            id: *request_id,
            operation: "diagnostics".to_string(),
            active_virtual: mapped.document.clone(),
            open_virtuals: open_virtuals.clone(),
            settings: settings.clone(),
            workspace_roots: workspace_roots.to_vec(),
            project_generation,
            project_fingerprint: Some(project_fingerprint.to_string()),
            project_reset_reason: Some(project_reset_reason.to_string()),
            params: Value::Null,
        });
        mapped_requests.push(mapped);
    }
    let languages = mapped_requests
        .iter()
        .map(|mapped| mapped.document.language_id.as_str())
        .collect::<Vec<_>>()
        .join(",");
    if let Some(connection) = connection {
        log_debug_only(
            connection,
            settings,
            format!(
                "[asp-lsp] embeddedDiagnostics.virtuals: uri={uri}, virtuals={virtual_count}, requests={}, languages={languages}, parallelism={parallelism}",
                mapped_requests.len()
            ),
        )?;
        log_debug_only(
            connection,
            settings,
            format!(
                "[asp-lsp] sidecar.requestBatch: operation=diagnostics, requests={}, parallelism={parallelism}, languages={languages}",
                mapped_requests.len()
            ),
        )?;
    }

    let responses = sidecar.request_batch(requests, parallelism);
    let mut diagnostics = Vec::new();
    for (mapped, response) in mapped_requests.into_iter().zip(responses) {
        let response = match response {
            Ok(response) => response,
            Err(error) => {
                if !error.contains("embedded sidecar dist/sidecar.js was not found") {
                    eprintln!("embedded sidecar diagnostics failed: {error}");
                }
                continue;
            }
        };
        log_sidecar_cache_stats(
            connection,
            settings,
            "diagnostics",
            &mapped.document.language_id,
            response.cache_stats.as_ref(),
        )?;
        let items = response
            .result
            .and_then(|result| result.as_array().cloned())
            .unwrap_or_default();
        diagnostics.extend(
            items
                .into_iter()
                .filter_map(|diagnostic| mapped.remap_diagnostic(diagnostic)),
        );
    }
    Ok(diagnostics)
}

fn collect_fast_embedded_semantic_tokens(
    virtuals: Vec<MappedVirtualDocument>,
    range: Option<TextRange>,
) -> Vec<DecodedSemanticToken> {
    let mut tokens = Vec::new();
    for mapped in virtuals {
        let virtual_tokens = match mapped.document.language_id.as_str() {
            "css" => css_semantic_token_values(&mapped.document.text),
            "javascript" | "jscript" => js_semantic_token_values(&mapped.document.text),
            _ => Vec::new(),
        };
        if virtual_tokens.is_empty() {
            continue;
        }
        let remapped = mapped.remap_lsp_value(Value::Array(virtual_tokens));
        for token in remapped.as_array().into_iter().flatten() {
            let Some(decoded) = decoded_semantic_token_from_value(token) else {
                continue;
            };
            if range.as_ref().is_none_or(|range| {
                semantic_token_starts_in_range(
                    &decoded,
                    u64::from(range.start.line),
                    u64::from(range.start.character),
                    u64::from(range.end.line),
                    u64::from(range.end.character),
                )
            }) {
                tokens.push(decoded);
            }
        }
    }
    tokens
}

fn css_semantic_token_values(text: &str) -> Vec<Value> {
    let positions = byte_to_lsp_positions(text);
    let bytes = text.as_bytes();
    let mut tokens = Vec::new();
    let mut index = 0;
    while index < bytes.len() {
        if !bytes[index].is_ascii_alphabetic() && bytes[index] != b'-' {
            index += 1;
            continue;
        }
        let start = index;
        let mut end = index + 1;
        while end < bytes.len() && (bytes[end].is_ascii_alphabetic() || bytes[end] == b'-') {
            end += 1;
        }
        let mut cursor = end;
        while cursor < bytes.len() && bytes[cursor].is_ascii_whitespace() {
            cursor += 1;
        }
        if cursor < bytes.len()
            && bytes[cursor] == b':'
            && bytes[start..end].iter().any(u8::is_ascii_alphabetic)
        {
            if let Some(token) =
                semantic_token_value_from_byte_offsets(&positions, start, end, 6, 0)
            {
                tokens.push(token);
            }
        }
        index = end;
    }
    tokens
}

fn js_semantic_token_values(text: &str) -> Vec<Value> {
    let positions = byte_to_lsp_positions(text);
    let bytes = text.as_bytes();
    let mut tokens = Vec::new();
    let mut index = 0;
    let mut previous_keyword: Option<&str> = None;
    while index < bytes.len() {
        let byte = bytes[index];
        if byte.is_ascii_whitespace() {
            index += 1;
            continue;
        }
        if byte == b'/' && bytes.get(index + 1) == Some(&b'/') {
            index += 2;
            while index < bytes.len() && bytes[index] != b'\n' {
                index += 1;
            }
            previous_keyword = None;
            continue;
        }
        if byte == b'/' && bytes.get(index + 1) == Some(&b'*') {
            index += 2;
            while index + 1 < bytes.len() && !(bytes[index] == b'*' && bytes[index + 1] == b'/') {
                index += 1;
            }
            index = (index + 2).min(bytes.len());
            previous_keyword = None;
            continue;
        }
        if matches!(byte, b'\'' | b'"' | b'`') {
            let quote = byte;
            index += 1;
            while index < bytes.len() {
                if bytes[index] == b'\\' {
                    index = (index + 2).min(bytes.len());
                    continue;
                }
                if bytes[index] == quote {
                    index += 1;
                    break;
                }
                index += 1;
            }
            previous_keyword = None;
            continue;
        }
        if is_js_identifier_start(byte) {
            let start = index;
            index += 1;
            while index < bytes.len() && is_js_identifier_part(bytes[index]) {
                index += 1;
            }
            let word = &text[start..index];
            if is_js_keyword(word) {
                previous_keyword = Some(word);
                continue;
            }
            let next = next_non_whitespace(bytes, index);
            let previous = previous_non_whitespace(bytes, start);
            let token_type = if previous_keyword == Some("function") {
                3
            } else if previous_keyword == Some("class") {
                4
            } else if previous == Some(b'.') {
                6
            } else if next == Some(b'(') {
                3
            } else {
                previous_keyword = None;
                continue;
            };
            push_semantic_token_value(&positions, &mut tokens, start, index, token_type, 0);
            previous_keyword = None;
            continue;
        }
        previous_keyword = None;
        index += 1;
    }
    tokens
}

fn push_semantic_token_value(
    positions: &[(usize, usize)],
    tokens: &mut Vec<Value>,
    start: usize,
    end: usize,
    token_type: u64,
    token_modifiers: u64,
) {
    if let Some(token) =
        semantic_token_value_from_byte_offsets(positions, start, end, token_type, token_modifiers)
    {
        tokens.push(token);
    }
}

fn semantic_token_value_from_byte_offsets(
    positions: &[(usize, usize)],
    start: usize,
    end: usize,
    token_type: u64,
    token_modifiers: u64,
) -> Option<Value> {
    let (start_line, start_character) = *positions.get(start)?;
    let (end_line, end_character) = *positions.get(end)?;
    Some(json!({
        "range": {
            "start": { "line": start_line, "character": start_character },
            "end": { "line": end_line, "character": end_character },
        },
        "tokenType": token_type,
        "tokenModifiers": token_modifiers,
    }))
}

fn byte_to_lsp_positions(text: &str) -> Vec<(usize, usize)> {
    let mut positions = vec![(0, 0); text.len() + 1];
    let mut line = 0;
    let mut character = 0;
    for (byte_index, ch) in text.char_indices() {
        positions[byte_index] = (line, character);
        if ch == '\n' {
            line += 1;
            character = 0;
        } else {
            character += ch.len_utf16();
        }
        let next = byte_index + ch.len_utf8();
        for position in positions.iter_mut().take(next + 1).skip(byte_index + 1) {
            *position = (line, character);
        }
    }
    positions[text.len()] = (line, character);
    positions
}

fn is_js_identifier_start(byte: u8) -> bool {
    byte.is_ascii_alphabetic() || matches!(byte, b'_' | b'$')
}

fn is_js_identifier_part(byte: u8) -> bool {
    byte.is_ascii_alphanumeric() || matches!(byte, b'_' | b'$')
}

fn is_js_keyword(word: &str) -> bool {
    matches!(
        word,
        "async"
            | "await"
            | "break"
            | "case"
            | "catch"
            | "class"
            | "const"
            | "continue"
            | "debugger"
            | "default"
            | "delete"
            | "do"
            | "else"
            | "export"
            | "extends"
            | "finally"
            | "for"
            | "function"
            | "if"
            | "import"
            | "in"
            | "instanceof"
            | "let"
            | "new"
            | "return"
            | "switch"
            | "throw"
            | "try"
            | "typeof"
            | "var"
            | "void"
            | "while"
            | "with"
            | "yield"
    )
}

fn next_non_whitespace(bytes: &[u8], mut index: usize) -> Option<u8> {
    while index < bytes.len() {
        if !bytes[index].is_ascii_whitespace() {
            return Some(bytes[index]);
        }
        index += 1;
    }
    None
}

fn previous_non_whitespace(bytes: &[u8], index: usize) -> Option<u8> {
    let mut cursor = index.checked_sub(1)?;
    loop {
        if !bytes[cursor].is_ascii_whitespace() {
            return Some(bytes[cursor]);
        }
        cursor = cursor.checked_sub(1)?;
    }
}

struct EmbeddedSidecar {
    processes: Vec<EmbeddedSidecarProcess>,
}

impl Default for EmbeddedSidecar {
    fn default() -> Self {
        Self {
            processes: Vec::new(),
        }
    }
}

impl EmbeddedSidecar {
    fn is_running(&self) -> bool {
        !self.processes.is_empty()
    }

    fn request(&mut self, request: EmbeddedRequest) -> Result<EmbeddedResponse, String> {
        self.ensure_process_count(1)?;
        request_with_process_retry(&mut self.processes[0], &request)
    }

    fn request_batch(
        &mut self,
        requests: Vec<EmbeddedRequest>,
        parallelism: usize,
    ) -> Vec<Result<EmbeddedResponse, String>> {
        if requests.is_empty() {
            return Vec::new();
        }
        let worker_count = parallelism.max(1).min(requests.len());
        self.request_batch_with_worker_count(requests, worker_count)
    }

    fn request_batch_with_worker_count(
        &mut self,
        requests: Vec<EmbeddedRequest>,
        worker_count: usize,
    ) -> Vec<Result<EmbeddedResponse, String>> {
        let request_count = requests.len();
        if worker_count == 1 {
            return requests
                .into_iter()
                .map(|request| self.request(request))
                .collect();
        }

        if let Err(error) = self.ensure_process_count(worker_count) {
            return requests.into_iter().map(|_| Err(error.clone())).collect();
        }

        let mut assignments = (0..worker_count).map(|_| Vec::new()).collect::<Vec<_>>();
        for (index, request) in requests.into_iter().enumerate() {
            assignments[index % worker_count].push((index, request));
        }

        let mut processes = std::mem::take(&mut self.processes);
        let mut ordered = Vec::new();
        thread::scope(|scope| {
            let mut handles = Vec::new();
            for (process, jobs) in processes.iter_mut().take(worker_count).zip(assignments) {
                handles.push(scope.spawn(move || {
                    let mut results = Vec::new();
                    for (index, request) in jobs {
                        results.push((index, request_with_process_retry(process, &request)));
                    }
                    results
                }));
            }
            for handle in handles {
                match handle.join() {
                    Ok(results) => ordered.extend(results),
                    Err(_) => ordered.push((
                        usize::MAX,
                        Err("embedded sidecar worker panicked".to_string()),
                    )),
                }
            }
        });
        self.processes = processes;

        let mut responses = (0..request_count)
            .map(|_| Err("embedded sidecar response is missing".to_string()))
            .collect::<Vec<_>>();
        for (index, result) in ordered {
            if index < responses.len() {
                responses[index] = result;
            }
        }
        responses
    }

    fn ensure_process_count(&mut self, count: usize) -> Result<(), String> {
        while self.processes.len() < count {
            self.processes.push(EmbeddedSidecarProcess::start()?);
        }
        Ok(())
    }
}

fn request_with_process_retry(
    process: &mut EmbeddedSidecarProcess,
    request: &EmbeddedRequest,
) -> Result<EmbeddedResponse, String> {
    request_with_process(process, request).or_else(|error| {
        *process = EmbeddedSidecarProcess::start()?;
        request_with_process(process, request)
            .map_err(|retry_error| format!("{error}; retry failed: {retry_error}"))
    })
}

fn request_with_process(
    process: &mut EmbeddedSidecarProcess,
    request: &EmbeddedRequest,
) -> Result<EmbeddedResponse, String> {
    let response = process.request(request)?;
    if response.ok {
        Ok(response)
    } else {
        Err(response
            .error
            .unwrap_or_else(|| "embedded sidecar failed".to_string()))
    }
}

struct EmbeddedSidecarProcess {
    child: Child,
    stdin: ChildStdin,
    stdout: ChildStdout,
}

impl EmbeddedSidecarProcess {
    fn start() -> Result<Self, String> {
        let sidecar_path = resolve_sidecar_path()?;
        let mut child = Command::new("node")
            .arg(sidecar_path)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::inherit())
            .spawn()
            .map_err(|error| format!("failed to start embedded sidecar: {error}"))?;
        let stdin = child
            .stdin
            .take()
            .ok_or_else(|| "embedded sidecar stdin is unavailable".to_string())?;
        let stdout = child
            .stdout
            .take()
            .ok_or_else(|| "embedded sidecar stdout is unavailable".to_string())?;
        Ok(Self {
            child,
            stdin,
            stdout,
        })
    }

    fn request(&mut self, request: &EmbeddedRequest) -> Result<EmbeddedResponse, String> {
        write_sidecar_frame(&mut self.stdin, request)?;
        read_sidecar_frame(&mut self.stdout)
    }
}

impl Drop for EmbeddedSidecarProcess {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
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
                .send(Response::new_ok(request.id, state.backend_status()).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        RA_VIEW_FILE_TEXT_METHOD => {
            let uri = request_text_document_uri(&request.params);
            connection
                .sender
                .send(Response::new_ok(request.id, state.ide.view_file_text(&uri)?).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        RA_VIEW_SYNTAX_TREE_METHOD => {
            let uri = request_text_document_uri(&request.params);
            connection
                .sender
                .send(Response::new_ok(request.id, state.ide.view_syntax_tree(&uri)?).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        RA_ANALYZER_STATUS_METHOD => {
            connection
                .sender
                .send(Response::new_ok(request.id, state.ide.analyzer_status()).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        RA_MEMORY_USAGE_METHOD => {
            connection
                .sender
                .send(Response::new_ok(request.id, state.ide.memory_usage()).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        RA_OPEN_SERVER_LOGS_METHOD => {
            connection
                .sender
                .send(Response::new_ok(request.id, json!({ "ok": true })).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        RA_MATCHING_BRACE_METHOD => {
            let uri = request_text_document_uri(&request.params);
            let position = request_position(&request.params)?;
            connection
                .sender
                .send(
                    Response::new_ok(request.id, state.ide.matching_brace(&uri, position)?).into(),
                )
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        EXPERIMENTAL_PARENT_MODULE_METHOD => {
            let uri = request_text_document_uri(&request.params);
            connection
                .sender
                .send(Response::new_ok(request.id, state.ide.parent_modules(&uri)?).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        EXPERIMENTAL_CHILD_MODULES_METHOD => {
            let uri = request_text_document_uri(&request.params);
            connection
                .sender
                .send(Response::new_ok(request.id, state.ide.child_modules(&uri)?).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        EXPERIMENTAL_JOIN_LINES_METHOD => {
            let uri = request_text_document_uri(&request.params);
            let ranges = request_ranges_or_default(&request.params)?;
            connection
                .sender
                .send(Response::new_ok(request.id, state.ide.join_lines(&uri, &ranges)?).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        EXPERIMENTAL_ON_ENTER_METHOD => {
            let uri = request_text_document_uri(&request.params);
            let position = request_position(&request.params)?;
            connection
                .sender
                .send(Response::new_ok(request.id, state.ide.on_enter(&uri, position)?).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        EXPERIMENTAL_MOVE_ITEM_METHOD => {
            let uri = request_text_document_uri(&request.params);
            let position = request_position(&request.params)?;
            let direction = request
                .params
                .get("direction")
                .and_then(Value::as_str)
                .unwrap_or("down");
            connection
                .sender
                .send(
                    Response::new_ok(request.id, state.ide.move_item(&uri, position, direction)?)
                        .into(),
                )
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        EXPERIMENTAL_EXTERNAL_DOCS_METHOD => {
            let uri = request_text_document_uri(&request.params);
            let position = request_position(&request.params)?;
            connection
                .sender
                .send(Response::new_ok(request.id, state.ide.external_docs(&uri, position)?).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        EXPERIMENTAL_SSR_METHOD => {
            let uri = request_text_document_uri(&request.params);
            let (search, replace) = ssr_terms(&request.params)?;
            connection
                .sender
                .send(Response::new_ok(request.id, state.ide.ssr(&uri, &search, &replace)?).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/completion" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let position = request_position(&request.params)?;
            let result = merge_lsp_arrays(
                state.ide.completion(&uri, position)?,
                state.embedded_position_feature(connection, &uri, "completion", position)?,
            );
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "completionItem/resolve" => {
            let result = state.ide.resolve_completion_item(request.params)?;
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/hover" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let position = request_position(&request.params)?;
            let result = state.ide.hover(&uri, position)?;
            let result = if result.is_null() {
                state
                    .embedded_position_feature(connection, &uri, "hover", position)?
                    .unwrap_or(Value::Null)
            } else {
                result
            };
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/definition" | "textDocument/declaration" | "textDocument/implementation" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let position = request_position(&request.params)?;
            let result = state.ide.definition(&uri, position)?;
            let result = if result.is_null() {
                state
                    .embedded_position_feature(connection, &uri, "definition", position)?
                    .unwrap_or(Value::Null)
            } else {
                result
            };
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/typeDefinition" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let position = request_position(&request.params)?;
            let result = state.ide.type_definition(&uri, position)?;
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/signatureHelp" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let position = request_position(&request.params)?;
            let result = state.ide.signature_help(&uri, position)?;
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/documentSymbol" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let result = merge_lsp_arrays(
                state.ide.document_symbols(&uri)?,
                Some(state.embedded_document_feature(connection, &uri, "documentSymbols")?),
            );
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/foldingRange" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let result = merge_lsp_arrays(
                state.ide.folding_ranges(&uri)?,
                Some(state.embedded_document_feature(connection, &uri, "foldingRanges")?),
            );
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/documentHighlight" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let position = request_position(&request.params)?;
            let result = merge_lsp_arrays(
                state.ide.document_highlights(&uri, position)?,
                state.embedded_document_highlights(connection, &uri, position)?,
            );
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/references" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let position = request_position(&request.params)?;
            let include_declaration = request
                .params
                .pointer("/context/includeDeclaration")
                .and_then(Value::as_bool)
                .unwrap_or(true);
            let result = state.ide.references(&uri, position, include_declaration)?;
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/prepareRename" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let position = request_position(&request.params)?;
            let result =
                if let Some(result) = state.embedded_prepare_rename(connection, &uri, position)? {
                    result
                } else {
                    state.ide.prepare_rename(&uri, position)?
                };
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/rename" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let position = request_position(&request.params)?;
            let new_name = pointer_string(&request.params, "/newName");
            let result = if let Some(result) =
                state.embedded_rename(connection, &uri, position, &new_name)?
            {
                result
            } else {
                state.ide.rename(&uri, position, &new_name)?
            };
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "workspace/symbol" => {
            let query = pointer_string(&request.params, "/query");
            let result = state.ide.workspace_symbols(&query)?;
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/semanticTokens/full" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let result = state.semantic_tokens_full(&uri)?;
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/semanticTokens/full/delta" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let previous_result_id = pointer_string(&request.params, "/previousResultId");
            let result = state.semantic_tokens_delta(&uri, &previous_result_id)?;
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/semanticTokens/range" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let range = request_range(&request.params)?;
            let result = state.semantic_tokens_range(&uri, range)?;
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/documentColor" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let result = state.embedded_document_feature(connection, &uri, "documentColors")?;
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/colorPresentation" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let range = request_range(&request.params)?;
            let color = request.params.get("color").cloned().unwrap_or(Value::Null);
            let result = state.embedded_color_presentations(connection, &uri, color, range)?;
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/diagnostic" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let result = state.text_document_diagnostic(&uri)?;
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/documentLink" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let result = state.ide.document_links(&uri)?;
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "documentLink/resolve" | "inlayHint/resolve" | "codeAction/resolve" => {
            connection
                .sender
                .send(Response::new_ok(request.id, request.params).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "codeLens/resolve" => {
            let result = resolve_code_lens(&state.ide, &state.settings, request.params)?;
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/selectionRange" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let positions = request_positions(&request.params)?;
            let result = state
                .embedded_selection_ranges(connection, &uri, &positions)?
                .unwrap_or_else(|| Value::Array(Vec::new()));
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/inlayHint" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let range = request_range(&request.params)?;
            let result = state.ide.inlay_hints(&uri, range, &state.settings)?;
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/codeLens" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let result = state.ide.code_lenses(&uri)?;
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/codeAction" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let range = request_range(&request.params)?;
            let result = state.ide.code_actions(&uri, range)?;
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/prepareCallHierarchy" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let position = request_position(&request.params)?;
            let result = state.ide.call_hierarchy_item(&uri, position)?;
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/prepareTypeHierarchy" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let position = request_position(&request.params)?;
            let result = state.ide.type_hierarchy_item(&uri, position)?;
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "callHierarchy/incomingCalls" => {
            let result = state.ide.call_hierarchy_incoming(&request.params["item"])?;
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "callHierarchy/outgoingCalls" => {
            let result = state.ide.call_hierarchy_outgoing(&request.params["item"])?;
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "typeHierarchy/supertypes" | "typeHierarchy/subtypes" => {
            connection
                .sender
                .send(Response::new_ok(request.id, state.ide.type_hierarchy_relations()).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/moniker" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let position = request_position(&request.params)?;
            let result = state.ide.monikers(&uri, position)?;
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/inlineValue" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let range = request_range(&request.params)?;
            let result = state.ide.inline_values(&uri, range)?;
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/formatting"
        | "textDocument/rangeFormatting"
        | "textDocument/onTypeFormatting" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let range = match request.method.as_str() {
                "textDocument/rangeFormatting" => Some(request_range(&request.params)?),
                "textDocument/onTypeFormatting" => {
                    let position = request_position(&request.params)?;
                    Some(TextRange {
                        start: TextPosition {
                            line: position.line,
                            character: 0,
                        },
                        end: position,
                    })
                }
                _ => None,
            };
            let options = request.params.get("options").unwrap_or(&Value::Null);
            let result = match request.method.as_str() {
                "textDocument/formatting" => state.document_formatting(&uri, options)?,
                "textDocument/rangeFormatting" => state
                    .embedded_range_formatting(connection, &uri, range.expect("range"), options)?
                    .filter(non_empty_lsp_array)
                    .map(Ok)
                    .unwrap_or_else(|| {
                        state
                            .ide
                            .formatting_edits(&uri, range, options, &state.settings)
                    })?,
                "textDocument/onTypeFormatting" => {
                    let position = request_position(&request.params)?;
                    let character = pointer_string(&request.params, "/ch");
                    state
                        .embedded_on_type_formatting(
                            connection, &uri, position, &character, options,
                        )?
                        .filter(non_empty_lsp_array)
                        .map(Ok)
                        .unwrap_or_else(|| {
                            state
                                .ide
                                .formatting_edits(&uri, range, options, &state.settings)
                        })?
                }
                _ => Value::Array(Vec::new()),
            };
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/willSaveWaitUntil" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let result = if format_on_save_enabled(&state.settings) {
                state
                    .ide
                    .formatting_edits(&uri, None, &Value::Null, &state.settings)?
            } else {
                Value::Array(Vec::new())
            };
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/linkedEditingRange" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let position = request_position(&request.params)?;
            let result = state
                .embedded_position_feature(connection, &uri, "linkedEditingRanges", position)?
                .unwrap_or(Value::Null);
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "workspace/executeCommand" => {
            let command = pointer_string(&request.params, "/command");
            let result = match state.execute_command(&command) {
                Ok(result) => result,
                Err(error) => {
                    connection
                        .sender
                        .send(
                            Response::new_err(request.id, ErrorCode::InvalidRequest as i32, error)
                                .into(),
                        )
                        .map_err(|error| error.to_string())?;
                    return Ok(false);
                }
            };
            if command == "aspLsp.server.reindexWorkspace" {
                state.log_workspace_index_completed(connection, "executeCommand")?;
            }
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            publish_backend_status(connection, state)?;
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
        "textDocument/didSave" => {
            let uri = pointer_string(&notification.params, "/textDocument/uri");
            if let Some(text) = notification.params.get("text").and_then(Value::as_str) {
                state
                    .ide
                    .replace_document_text(uri.clone(), text.to_string());
            }
            state.publish_fast_diagnostics(connection, &uri)?;
            state.schedule_diagnostics(uri);
        }
        "textDocument/didClose" => {
            let uri = pointer_string(&notification.params, "/textDocument/uri");
            state.ide.close_document(&uri);
            state.clear_semantic_tokens(&uri);
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
            publish_backend_status(connection, state)?;
        }
        "workspace/didCreateFiles" | "workspace/didRenameFiles" | "workspace/didDeleteFiles" => {
            state.refresh_workspace_index()?;
            state.log_workspace_index_completed(connection, "fileOperations")?;
        }
        "workspace/didChangeWatchedFiles" => {
            let affects_workspace_index =
                watched_file_changes_affect_workspace_index(&notification.params);
            let changed_workspace_uris = changed_indexed_workspace_uris(&notification.params);
            for uri in changed_workspace_uris
                .iter()
                .filter(|uri| is_include_uri(uri))
            {
                let impact = state.ide.include_impact_for_change(uri)?;
                log_include_impact(connection, &state.settings, &impact)?;
            }
            if affects_workspace_index {
                state.refresh_workspace_index()?;
                state.log_workspace_index_completed(connection, "watchedFiles")?;
            } else {
                state.bump_sidecar_project_generation("watchedFiles");
            }
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
        "completionProvider": {
            "resolveProvider": true,
            "triggerCharacters": ["<", ".", "\"", "'", ":", "#", "(", " "],
        },
        "signatureHelpProvider": {
            "triggerCharacters": ["(", ",", " "],
        },
        "hoverProvider": true,
        "definitionProvider": true,
        "declarationProvider": true,
        "typeDefinitionProvider": true,
        "implementationProvider": true,
        "documentSymbolProvider": true,
        "foldingRangeProvider": true,
        "documentHighlightProvider": true,
        "referencesProvider": true,
        "renameProvider": { "prepareProvider": true },
        "workspaceSymbolProvider": true,
        "semanticTokensProvider": {
            "legend": {
                "tokenTypes": [
                    "keyword",
                    "variable",
                    "parameter",
                    "function",
                    "class",
                    "method",
                    "property",
                    "comment",
                    "string",
                    "operator",
                    "namespace",
                    "interface",
                    "enum",
                    "enumMember",
                    "typeAlias",
                    "typeParameter",
                ],
                "tokenModifiers": ["public", "private", "readonly", "library", "byref", "byval"],
            },
            "full": { "delta": true },
            "range": true,
        },
        "documentLinkProvider": { "resolveProvider": true },
        "codeActionProvider": {
            "resolveProvider": true,
            "codeActionKinds": [
                "quickfix",
                "refactor",
                "source",
                "source.organizeImports",
                "source.organizeImports.aspLsp.javascript",
            ],
        },
        "codeLensProvider": { "resolveProvider": true },
        "colorProvider": true,
        "selectionRangeProvider": true,
        "linkedEditingRangeProvider": true,
        "inlayHintProvider": { "resolveProvider": true },
        "callHierarchyProvider": true,
        "typeHierarchyProvider": true,
        "monikerProvider": true,
        "inlineValueProvider": true,
        "documentFormattingProvider": true,
        "documentRangeFormattingProvider": true,
        "documentOnTypeFormattingProvider": {
            "firstTriggerCharacter": "\n",
            "moreTriggerCharacter": [">"],
        },
        "textDocumentSync": {
            "openClose": true,
            "change": 2,
            "willSave": true,
            "willSaveWaitUntil": true,
            "save": { "includeText": true },
        },
        "workspace": {
            "fileOperations": {
                "didCreate": { "filters": file_operation_filters() },
                "didRename": { "filters": file_operation_filters() },
                "didDelete": { "filters": file_operation_filters() },
            },
        },
        "executeCommandProvider": {
            "commands": [
                "aspLsp.server.reindexWorkspace",
                "aspLsp.server.clearCache",
                "aspLsp.server.clearProcessCache",
            ],
        },
        "experimental": {
            "rust-analyzer": {
                "viewFileText": true,
                "viewSyntaxTree": true,
                "analyzerStatus": true,
                "memoryUsage": true,
                "openServerLogs": true,
                "matchingBrace": true,
            },
            "asp-lsp": {
                "parentModule": true,
                "childModules": true,
                "joinLines": true,
                "onEnter": true,
                "moveItem": true,
                "externalDocs": true,
                "ssr": true,
            },
        },
        "positionEncoding": "utf-16",
    })
}

fn file_operation_filters() -> Vec<Value> {
    vec![json!({
        "scheme": "file",
        "pattern": {
            "glob": "**/*.{asp,asa,inc}",
        },
    })]
}

fn settings_from_initialize(params: &Value) -> Value {
    params
        .get("initializationOptions")
        .and_then(|options| options.get("settings"))
        .cloned()
        .unwrap_or_else(|| json!({}))
}

fn workspace_roots_from_initialize(params: &Value) -> Vec<String> {
    if let Some(folders) = params.get("workspaceFolders").and_then(Value::as_array) {
        return folders
            .iter()
            .filter_map(|folder| {
                folder
                    .get("uri")
                    .and_then(Value::as_str)
                    .map(str::to_string)
            })
            .collect();
    }
    params
        .get("rootUri")
        .and_then(Value::as_str)
        .map(|uri| vec![uri.to_string()])
        .unwrap_or_default()
}

fn watched_file_changes_affect_workspace_index(params: &Value) -> bool {
    !changed_indexed_workspace_uris(params).is_empty()
}

fn changed_indexed_workspace_uris(params: &Value) -> Vec<String> {
    params
        .get("changes")
        .and_then(Value::as_array)
        .into_iter()
        .flatten()
        .filter_map(|change| change.get("uri").and_then(Value::as_str))
        .filter(|uri| is_indexed_workspace_uri(uri))
        .map(ToString::to_string)
        .collect()
}

fn is_indexed_workspace_uri(uri: &str) -> bool {
    let lower = uri.to_lowercase();
    matches!(
        lower.rsplit_once('.').map(|(_, extension)| extension),
        Some("asp" | "asa" | "inc")
    )
}

fn is_include_uri(uri: &str) -> bool {
    uri.to_lowercase().ends_with(".inc")
}

fn workspace_max_index_files(settings: &Value) -> usize {
    settings
        .get("workspace")
        .and_then(|workspace| workspace.get("maxIndexFiles"))
        .and_then(Value::as_u64)
        .and_then(|value| usize::try_from(value).ok())
        .filter(|value| *value > 0)
        .unwrap_or(5_000)
}

fn embedded_parallelism(settings: &Value) -> usize {
    configured_parallelism(settings.get("embedded"), "parallelism", 4)
}

fn configured_parallelism(parent: Option<&Value>, key: &str, max_auto: usize) -> usize {
    let configured = parent
        .and_then(|value| value.get(key))
        .and_then(Value::as_u64)
        .and_then(|value| usize::try_from(value).ok());
    match configured {
        Some(0) | None => auto_parallelism(max_auto),
        Some(value) => value.max(1).min(max_auto),
    }
}

fn auto_parallelism(max_auto: usize) -> usize {
    thread::available_parallelism()
        .map(usize::from)
        .unwrap_or(2)
        .saturating_div(2)
        .max(1)
        .min(max_auto)
}

fn index_workspace_files(roots: &[String], max_files: usize) -> Result<Vec<IndexedFile>, String> {
    let mut files = Vec::new();
    for root in roots {
        let Some(path) = file_uri_to_path(root) else {
            continue;
        };
        collect_workspace_files(&path, &mut files, max_files)?;
        if files.len() >= max_files {
            break;
        }
    }
    Ok(files)
}

fn sidecar_project_files(roots: &[String]) -> Vec<DiskSourceMetadata> {
    const MAX_SIDECAR_PROJECT_FILES: usize = 2_000;
    let mut files = Vec::new();
    for root in roots {
        let Some(path) = file_uri_to_path(root) else {
            continue;
        };
        collect_sidecar_project_files(&path, &mut files, MAX_SIDECAR_PROJECT_FILES);
        if files.len() >= MAX_SIDECAR_PROJECT_FILES {
            break;
        }
    }
    files
}

fn collect_sidecar_project_files(
    path: &Path,
    files: &mut Vec<DiskSourceMetadata>,
    max_files: usize,
) {
    if files.len() >= max_files {
        return;
    }
    let Ok(metadata) = fs::metadata(path) else {
        return;
    };
    if metadata.is_file() {
        if is_sidecar_project_file(path) {
            files.push(DiskSourceMetadata {
                file_name: normalize_file_name(path),
                mtime_ms: metadata_mtime_ms(&metadata),
                size: metadata.len(),
            });
        }
        return;
    }
    if !metadata.is_dir() {
        return;
    }
    let Some(name) = path.file_name().and_then(|name| name.to_str()) else {
        return;
    };
    if matches!(name, ".git" | "node_modules" | "target" | "dist") {
        return;
    }
    let Ok(entries) = fs::read_dir(path) else {
        return;
    };
    for entry in entries.flatten() {
        collect_sidecar_project_files(&entry.path(), files, max_files);
        if files.len() >= max_files {
            break;
        }
    }
}

fn collect_workspace_files(
    path: &Path,
    files: &mut Vec<IndexedFile>,
    max_files: usize,
) -> Result<(), String> {
    if files.len() >= max_files {
        return Ok(());
    }
    let Ok(metadata) = fs::metadata(path) else {
        return Ok(());
    };
    if metadata.is_file() {
        if is_asp_like_file(path) {
            if let Ok(text) = fs::read_to_string(path) {
                files.push(IndexedFile {
                    uri: path_to_file_uri(path),
                    text,
                    source: DiskSourceMetadata {
                        file_name: normalize_file_name(path),
                        mtime_ms: metadata_mtime_ms(&metadata),
                        size: metadata.len(),
                    },
                });
            }
        }
        return Ok(());
    }
    if !metadata.is_dir() {
        return Ok(());
    }
    let Some(name) = path.file_name().and_then(|name| name.to_str()) else {
        return Ok(());
    };
    if matches!(name, ".git" | "node_modules" | "target" | "dist") {
        return Ok(());
    }
    let Ok(entries) = fs::read_dir(path) else {
        return Ok(());
    };
    for entry in entries.flatten() {
        collect_workspace_files(&entry.path(), files, max_files)?;
        if files.len() >= max_files {
            break;
        }
    }
    Ok(())
}

fn is_asp_like_file(path: &Path) -> bool {
    path.extension()
        .and_then(|extension| extension.to_str())
        .is_some_and(|extension| {
            matches!(
                extension.to_ascii_lowercase().as_str(),
                "asp" | "asa" | "inc"
            )
        })
}

fn is_sidecar_project_file(path: &Path) -> bool {
    let Some(file_name) = path.file_name().and_then(|name| name.to_str()) else {
        return false;
    };
    if matches!(
        file_name,
        "tsconfig.json" | "jsconfig.json" | "package.json"
    ) {
        return true;
    }
    path.extension()
        .and_then(|extension| extension.to_str())
        .is_some_and(|extension| {
            matches!(
                extension.to_ascii_lowercase().as_str(),
                "js" | "jsx" | "mjs" | "cjs" | "ts" | "tsx" | "mts" | "cts"
            )
        })
}

fn file_uri_to_path(uri: &str) -> Option<PathBuf> {
    uri.strip_prefix("file://").map(PathBuf::from)
}

fn path_to_file_uri(path: &Path) -> String {
    format!("file://{}", path.to_string_lossy())
}

fn normalize_file_name(path: &Path) -> String {
    path.to_string_lossy().replace('\\', "/")
}

fn metadata_mtime_ms(metadata: &fs::Metadata) -> f64 {
    metadata
        .modified()
        .ok()
        .and_then(|modified| modified.duration_since(UNIX_EPOCH).ok())
        .map(|duration| duration.as_secs_f64() * 1000.0)
        .unwrap_or(0.0)
}

fn sidecar_project_settings_key(settings: &Value) -> String {
    json!({
        "checkJs": settings.get("checkJs"),
        "javascript": settings.get("javascript"),
    })
    .to_string()
}

fn stable_hash(text: &str) -> String {
    let mut hash = 2166136261_u32;
    for unit in text.encode_utf16() {
        hash ^= u32::from(unit);
        hash = hash.wrapping_mul(16777619);
    }
    format!("{hash:08x}")
}

fn language_server_version() -> String {
    serde_json::from_str::<Value>(VSCODE_PACKAGE_JSON)
        .ok()
        .and_then(|package| {
            package
                .get("version")
                .and_then(Value::as_str)
                .map(str::to_string)
        })
        .unwrap_or_else(|| env!("CARGO_PKG_VERSION").to_string())
}

#[cfg(test)]
mod tests {
    use asp_ide::IncludeImpact;
    use serde_json::{json, Value};
    use std::{env, fs, path::PathBuf};

    fn temp_cache_dir(name: &str) -> PathBuf {
        let path = env::temp_dir().join(format!(
            "asp-lsp-disk-snapshot-{name}-{}",
            std::process::id()
        ));
        let _ = fs::remove_dir_all(&path);
        fs::create_dir_all(&path).expect("create cache dir");
        path
    }

    #[test]
    fn debug_output_level_uses_ordered_levels() {
        assert_eq!(
            super::debug_output_level(&Value::Null),
            super::DebugOutputLevel::Off
        );
        assert_eq!(
            super::debug_output_level(&json!({ "debug": { "output": "summary" } })),
            super::DebugOutputLevel::Summary
        );
        assert_eq!(
            super::debug_output_level(&json!({ "debug": { "output": "verbose" } })),
            super::DebugOutputLevel::Verbose
        );
        assert_eq!(
            super::debug_output_level(&json!({ "debug": { "output": "debug" } })),
            super::DebugOutputLevel::Debug
        );
        assert!(super::debug_output_at_least(
            &json!({ "debug": { "output": "debug" } }),
            super::DebugOutputLevel::Verbose
        ));
        assert!(!super::debug_output_at_least(
            &json!({ "debug": { "output": "verbose" } }),
            super::DebugOutputLevel::Debug
        ));
        assert!(!super::debug_output_at_least(
            &json!({ "debug": { "output": "off" } }),
            super::DebugOutputLevel::Summary
        ));
    }

    #[test]
    fn watched_asp_like_files_affect_workspace_index() {
        assert!(super::watched_file_changes_affect_workspace_index(&json!({
            "changes": [{ "uri": "file:///site/default.asp", "type": 2 }]
        })));
        assert!(super::watched_file_changes_affect_workspace_index(&json!({
            "changes": [{ "uri": "file:///site/shared.INC", "type": 2 }]
        })));
        assert!(!super::watched_file_changes_affect_workspace_index(
            &json!({
                "changes": [{ "uri": "file:///site/shared.js", "type": 2 }]
            })
        ));
        assert_eq!(
            super::changed_indexed_workspace_uris(&json!({
                "changes": [
                    { "uri": "file:///site/default.asp", "type": 2 },
                    { "uri": "file:///site/shared.inc", "type": 2 },
                    { "uri": "file:///site/client.js", "type": 2 },
                ]
            })),
            vec!["file:///site/default.asp", "file:///site/shared.inc"]
        );
        assert!(super::is_include_uri("file:///site/shared.INC"));
        assert!(!super::is_include_uri("file:///site/default.asp"));
    }

    #[test]
    fn include_impact_message_reports_counts_and_fingerprint() {
        let impact = IncludeImpact {
            changed_uri: "file:///site/shared.inc".to_string(),
            graph_fingerprint: "abc123".to_string(),
            affected_roots: vec!["file:///site/default.asp".to_string()],
            affected_documents: vec![
                "file:///site/default.asp".to_string(),
                "file:///site/nested.inc".to_string(),
            ],
        };

        let message = super::include_impact_message(&impact);
        assert!(message.contains("includeGraph.affected"));
        assert!(message.contains("changed=file:///site/shared.inc"));
        assert!(message.contains("roots=1"));
        assert!(message.contains("documents=2"));
        assert!(message.contains("fingerprint=abc123"));
    }

    #[test]
    fn sidecar_project_fingerprint_tracks_project_inputs_and_forced_resets() {
        let root = temp_cache_dir("sidecar-project-fingerprint");
        fs::write(
            root.join("default.asp"),
            "<script src=\"shared.js\"></script>",
        )
        .expect("write asp");
        fs::write(root.join("shared.js"), "var externalValue = \"text\";\n").expect("write js");

        let mut state = super::ServerState::default();
        state
            .set_settings(json!({ "checkJs": true }))
            .expect("settings");
        state
            .set_workspace_roots(vec![format!("file://{}", root.to_string_lossy())])
            .expect("workspace roots");
        let initial = state.sidecar_project_fingerprint.clone();

        fs::write(root.join("shared.js"), "var externalValue = 100;\n").expect("update js");
        state.bump_sidecar_project_generation("watchedFiles");
        let after_js_change = state.sidecar_project_fingerprint.clone();
        assert_ne!(after_js_change, initial);
        assert_eq!(state.sidecar_project_reset_reason, "watchedFiles");

        state
            .set_settings(json!({ "checkJs": false }))
            .expect("settings changed");
        let after_settings_change = state.sidecar_project_fingerprint.clone();
        assert_ne!(after_settings_change, after_js_change);
        assert_eq!(state.sidecar_project_reset_reason, "settings");

        state.bump_sidecar_project_generation("clearProcessCache");
        assert_ne!(
            state.sidecar_project_fingerprint, after_settings_change,
            "explicit process-cache clears must force a sidecar reset even when inputs are stable"
        );
        assert_eq!(state.sidecar_project_reset_reason, "clearProcessCache");

        let _ = fs::remove_dir_all(root);
    }

    #[test]
    fn sidecar_cache_reset_message_includes_reason_and_fingerprint() {
        let stats = json!({
            "generationReset": 1,
            "resetReason": "watchedFiles",
            "projectFingerprint": "abc123",
        });
        let message = super::sidecar_cache_stat_message(
            "sidecarCache.generationReset",
            "diagnostics",
            "javascript",
            1,
            stats.as_object().expect("stats object"),
        );
        assert!(message.contains("sidecarCache.generationReset"));
        assert!(message.contains("operation=diagnostics"));
        assert!(message.contains("language=javascript"));
        assert!(message.contains("reason=watchedFiles"));
        assert!(message.contains("fingerprint=abc123"));
    }

    #[test]
    fn semantic_token_delta_reuses_cached_result_for_unchanged_fingerprint() {
        let mut cache = super::SemanticTokenCache::default();
        let full = cache.full(
            "file:///site/default.asp",
            json!({ "data": [0, 0, 3, 1, 0] }),
            Some("fingerprint-a".to_string()),
        );
        let first_id = full["resultId"].as_str().expect("result id").to_string();

        let delta = cache
            .delta_from_cached("file:///site/default.asp", &first_id, Some("fingerprint-a"))
            .expect("cached delta");
        assert_ne!(delta["resultId"], first_id);
        assert_eq!(
            delta["edits"][0],
            json!({ "start": 5, "deleteCount": 0, "data": [] })
        );
        assert!(
            cache
                .delta_from_cached("file:///site/default.asp", &first_id, Some("fingerprint-b"))
                .is_none(),
            "changed fingerprint must not use cached delta"
        );
    }

    #[test]
    fn semantic_token_full_reuses_cached_result_for_unchanged_fingerprint() {
        let mut cache = super::SemanticTokenCache::default();
        let full = cache.full(
            "file:///site/default.asp",
            json!({ "data": [0, 0, 3, 1, 0] }),
            Some("fingerprint-a".to_string()),
        );
        let first_id = full["resultId"].as_str().expect("result id").to_string();

        let cached = cache
            .full_from_cached("file:///site/default.asp", Some("fingerprint-a"))
            .expect("cached full");
        assert_ne!(cached["resultId"], first_id);
        assert_eq!(cached["data"], json!([0, 0, 3, 1, 0]));
        assert!(
            cache
                .full_from_cached("file:///site/default.asp", Some("fingerprint-b"))
                .is_none(),
            "changed fingerprint must not use cached full tokens"
        );
    }

    #[test]
    fn semantic_token_range_reuses_cached_result_for_unchanged_fingerprint() {
        let mut cache = super::SemanticTokenCache::default();
        cache.full(
            "file:///site/default.asp",
            json!({
                "data": [
                    0, 0, 3, 1, 0,
                    1, 2, 4, 2, 0,
                    0, 8, 5, 3, 0,
                    1, 1, 6, 4, 0
                ]
            }),
            Some("fingerprint-a".to_string()),
        );

        let cached = cache
            .range_from_cached(
                "file:///site/default.asp",
                Some("fingerprint-a"),
                &super::TextRange {
                    start: super::TextPosition {
                        line: 1,
                        character: 0,
                    },
                    end: super::TextPosition {
                        line: 2,
                        character: 0,
                    },
                },
            )
            .expect("cached range");
        assert_eq!(cached["data"], json!([1, 2, 4, 2, 0, 0, 8, 5, 3, 0]));
        assert!(
            cache
                .range_from_cached(
                    "file:///site/default.asp",
                    Some("fingerprint-b"),
                    &super::TextRange {
                        start: super::TextPosition {
                            line: 1,
                            character: 0,
                        },
                        end: super::TextPosition {
                            line: 2,
                            character: 0,
                        },
                    },
                )
                .is_none(),
            "changed fingerprint must not use cached range tokens"
        );
    }

    #[test]
    fn workspace_max_index_files_reads_configuration() {
        assert_eq!(super::workspace_max_index_files(&Value::Null), 5_000);
        assert_eq!(
            super::workspace_max_index_files(&json!({ "workspace": { "maxIndexFiles": 2 } })),
            2
        );
        assert_eq!(
            super::workspace_max_index_files(&json!({ "workspace": { "maxIndexFiles": 0 } })),
            5_000
        );
    }

    #[test]
    fn embedded_parallelism_setting_uses_auto_serial_and_clamped_values() {
        let auto_embedded =
            super::embedded_parallelism(&json!({ "embedded": { "parallelism": 0 } }));
        assert!((1..=4).contains(&auto_embedded));
        assert_eq!(
            super::embedded_parallelism(&json!({ "embedded": { "parallelism": 1 } })),
            1
        );
        assert_eq!(
            super::embedded_parallelism(&json!({ "embedded": { "parallelism": 99 } })),
            4
        );
    }

    #[test]
    fn server_timeout_uses_only_scheduled_diagnostics() {
        let mut state = super::ServerState::default();
        assert_eq!(state.next_server_timeout(), None);

        state.schedule_diagnostics("file:///site/default.asp".to_string());
        assert!(state.next_server_timeout().is_some());
    }

    #[test]
    fn index_workspace_files_respects_configured_file_limit() {
        let root = env::temp_dir().join(format!("asp-lsp-index-limit-{}", std::process::id()));
        let _ = fs::remove_dir_all(&root);
        fs::create_dir_all(&root).expect("create temp root");
        fs::write(root.join("first.asp"), "<% Dim first %>").expect("write first");
        fs::write(root.join("second.asp"), "<% Dim second %>").expect("write second");
        fs::write(root.join("third.inc"), "<% Dim third %>").expect("write third");

        let root_uri = format!("file://{}", root.to_string_lossy());
        let files = super::index_workspace_files(&[root_uri], 2).expect("index workspace");

        assert_eq!(files.len(), 2);
        let _ = fs::remove_dir_all(root);
    }
}

fn format_on_save_enabled(settings: &Value) -> bool {
    settings
        .get("format")
        .and_then(|format| format.get("onSave"))
        .and_then(Value::as_bool)
        .unwrap_or(false)
}

#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
enum DebugOutputLevel {
    Off,
    Summary,
    Verbose,
    Debug,
}

fn debug_output_level(settings: &Value) -> DebugOutputLevel {
    match settings
        .get("debug")
        .and_then(|debug| debug.get("output"))
        .and_then(Value::as_str)
    {
        Some("summary") => DebugOutputLevel::Summary,
        Some("verbose") => DebugOutputLevel::Verbose,
        Some("debug") => DebugOutputLevel::Debug,
        _ => DebugOutputLevel::Off,
    }
}

fn debug_output_at_least(settings: &Value, minimum: DebugOutputLevel) -> bool {
    debug_output_level(settings) >= minimum
}

fn log_debug_at(
    connection: &Connection,
    settings: &Value,
    minimum: DebugOutputLevel,
    message: String,
) -> Result<(), String> {
    if !debug_output_at_least(settings, minimum) {
        return Ok(());
    }
    connection
        .sender
        .send(
            Notification::new(
                "window/logMessage".to_string(),
                json!({ "type": 3, "message": message }),
            )
            .into(),
        )
        .map_err(|error| error.to_string())
}

fn log_debug_summary(
    connection: &Connection,
    settings: &Value,
    message: String,
) -> Result<(), String> {
    log_debug_at(connection, settings, DebugOutputLevel::Verbose, message)
}

fn log_debug_only(
    connection: &Connection,
    settings: &Value,
    message: String,
) -> Result<(), String> {
    log_debug_at(connection, settings, DebugOutputLevel::Debug, message)
}

fn log_include_impact(
    connection: &Connection,
    settings: &Value,
    impact: &IncludeImpact,
) -> Result<(), String> {
    log_debug_summary(connection, settings, include_impact_message(impact))
}

fn include_impact_message(impact: &IncludeImpact) -> String {
    format!(
        "[asp-lsp] includeGraph.affected: changed={}, roots={}, documents={}, fingerprint={}",
        impact.changed_uri,
        impact.affected_roots.len(),
        impact.affected_documents.len(),
        impact.graph_fingerprint
    )
}

fn log_sidecar_cache_stats(
    connection: Option<&Connection>,
    settings: &Value,
    operation: &str,
    language_id: &str,
    stats: Option<&Value>,
) -> Result<(), String> {
    let Some(connection) = connection else {
        return Ok(());
    };
    let Some(stats) = stats.and_then(Value::as_object) else {
        return Ok(());
    };
    const STAT_EVENTS: &[(&str, &str)] = &[
        ("generationReset", "sidecarCache.generationReset"),
        ("semanticTokensHit", "sidecarCache.semanticTokens.hit"),
        ("semanticTokensMiss", "sidecarCache.semanticTokens.miss"),
        ("fileExistsHit", "sidecarCache.fileExists.hit"),
        ("fileExistsMiss", "sidecarCache.fileExists.miss"),
        ("readFileHit", "sidecarCache.readFile.hit"),
        ("readFileMiss", "sidecarCache.readFile.miss"),
        ("directoryExistsHit", "sidecarCache.directoryExists.hit"),
        ("directoryExistsMiss", "sidecarCache.directoryExists.miss"),
        ("getDirectoriesHit", "sidecarCache.getDirectories.hit"),
        ("getDirectoriesMiss", "sidecarCache.getDirectories.miss"),
        ("readDirectoryHit", "sidecarCache.readDirectory.hit"),
        ("readDirectoryMiss", "sidecarCache.readDirectory.miss"),
        ("realpathHit", "sidecarCache.realpath.hit"),
        ("realpathMiss", "sidecarCache.realpath.miss"),
    ];
    for (field, event_name) in STAT_EVENTS {
        let count = stats.get(*field).and_then(Value::as_u64).unwrap_or(0);
        if count == 0 {
            continue;
        }
        log_debug_summary(
            connection,
            settings,
            sidecar_cache_stat_message(event_name, operation, language_id, count, stats),
        )?;
    }
    Ok(())
}

fn sidecar_cache_stat_message(
    event_name: &str,
    operation: &str,
    language_id: &str,
    count: u64,
    stats: &serde_json::Map<String, Value>,
) -> String {
    let reset_context = if event_name == "sidecarCache.generationReset" {
        let reason = stats
            .get("resetReason")
            .and_then(Value::as_str)
            .unwrap_or("unknown");
        let fingerprint = stats
            .get("projectFingerprint")
            .and_then(Value::as_str)
            .unwrap_or("unknown");
        format!(" reason={reason} fingerprint={fingerprint}")
    } else {
        String::new()
    };
    format!(
        "[asp-lsp] {event_name}: operation={operation} language={language_id} count={count}{reset_context}"
    )
}

fn text_range(value: &Value) -> Option<TextRange> {
    Some(TextRange {
        start: text_position(value.get("start")?)?,
        end: text_position(value.get("end")?)?,
    })
}

fn request_position(params: &Value) -> Result<TextPosition, String> {
    params
        .get("position")
        .and_then(text_position)
        .ok_or_else(|| "position is required".to_string())
}

fn request_range(params: &Value) -> Result<TextRange, String> {
    params
        .get("range")
        .and_then(text_range)
        .ok_or_else(|| "range is required".to_string())
}

fn request_ranges_or_default(params: &Value) -> Result<Vec<TextRange>, String> {
    if let Some(ranges) = params.get("ranges").and_then(Value::as_array) {
        return ranges
            .iter()
            .map(|value| text_range(value).ok_or_else(|| "invalid range".to_string()))
            .collect();
    }
    Ok(vec![request_range(params)?])
}

fn ssr_terms(params: &Value) -> Result<(String, String), String> {
    let search = params.get("search").and_then(Value::as_str);
    let replace = params.get("replace").and_then(Value::as_str);
    if let (Some(search), Some(replace)) = (search, replace) {
        return Ok((search.to_string(), replace.to_string()));
    }
    let Some(query) = params.get("query").and_then(Value::as_str) else {
        return Err("ssr search/replace or query is required".to_string());
    };
    let Some((search, replace)) = query.split_once("==>>") else {
        return Err("ssr query must use `search ==>> replace`".to_string());
    };
    Ok((search.trim().to_string(), replace.trim().to_string()))
}

fn request_positions(params: &Value) -> Result<Vec<TextPosition>, String> {
    params
        .get("positions")
        .and_then(Value::as_array)
        .ok_or_else(|| "positions are required".to_string())?
        .iter()
        .map(|value| text_position(value).ok_or_else(|| "invalid position".to_string()))
        .collect()
}

fn merge_lsp_arrays(left: Value, right: Option<Value>) -> Value {
    let mut items = match left {
        Value::Array(items) => items,
        Value::Null => Vec::new(),
        value => vec![value],
    };
    match right {
        Some(Value::Array(values)) => items.extend(values),
        Some(Value::Null) | None => {}
        Some(value) => items.push(value),
    }
    Value::Array(items)
}

fn non_empty_lsp_array(value: &Value) -> bool {
    value.as_array().is_some_and(|items| !items.is_empty())
}

fn remap_workspace_edit(uri: &str, mapped: &MappedVirtualDocument, value: Value) -> Value {
    let Some(changes) = value.get("changes").and_then(Value::as_object) else {
        return mapped.remap_lsp_value(value);
    };
    let Some(edits) = changes.get(&mapped.document.uri).and_then(Value::as_array) else {
        return Value::Null;
    };
    let remapped = mapped.remap_lsp_value(Value::Array(edits.clone()));
    json!({
        "changes": {
            uri: remapped.as_array().cloned().unwrap_or_default(),
        },
    })
}

fn decoded_semantic_token_from_value(value: &Value) -> Option<DecodedSemanticToken> {
    let range = value.get("range")?;
    let start = range.get("start")?;
    let end = range.get("end")?;
    let line = start.get("line")?.as_u64()?;
    let character = start.get("character")?.as_u64()?;
    let end_line = end.get("line")?.as_u64()?;
    let end_character = end.get("character")?.as_u64()?;
    if line != end_line {
        return None;
    }
    Some(DecodedSemanticToken {
        line,
        character,
        length: end_character.saturating_sub(character),
        token_type: value.get("tokenType")?.as_u64()?,
        token_modifiers: value
            .get("tokenModifiers")
            .and_then(Value::as_u64)
            .unwrap_or(0),
    })
}

fn full_text_range(text: &str) -> Value {
    let (line, character) = utf16_position_at(text, utf16_len(text)).unwrap_or((0, 0));
    json!({
        "start": { "line": 0, "character": 0 },
        "end": { "line": line, "character": character },
    })
}

fn apply_lsp_text_edits(text: &str, edits: &[Value]) -> Result<String, String> {
    let mut edits = edits
        .iter()
        .filter_map(|edit| {
            let range = edit.get("range").and_then(text_range)?;
            let new_text = edit.get("newText")?.as_str()?.to_string();
            let start = utf16_offset_at(text, range.start.line, range.start.character)?;
            let end = utf16_offset_at(text, range.end.line, range.end.character)?;
            Some((start, end, new_text))
        })
        .collect::<Vec<_>>();
    edits.sort_by_key(|(start, _, _)| std::cmp::Reverse(*start));
    let mut output = text.to_string();
    for (start, end, new_text) in edits {
        let start_byte = utf16_to_byte_offset(&output, start)?;
        let end_byte = utf16_to_byte_offset(&output, end)?;
        output.replace_range(start_byte..end_byte, &new_text);
    }
    Ok(output)
}

fn utf16_len(text: &str) -> usize {
    text.chars().map(char::len_utf16).sum()
}

fn utf16_offset_at(text: &str, line: u32, character: u32) -> Option<usize> {
    let mut current_line = 0_u32;
    let mut current_character = 0_u32;
    let mut offset = 0_usize;
    for current in text.chars() {
        if current_line == line && current_character == character {
            return Some(offset);
        }
        offset += current.len_utf16();
        if current == '\n' {
            current_line += 1;
            current_character = 0;
        } else {
            current_character += u32::try_from(current.len_utf16()).ok()?;
        }
    }
    (current_line == line && current_character == character).then_some(offset)
}

fn utf16_position_at(text: &str, target_offset: usize) -> Option<(usize, usize)> {
    let mut offset = 0_usize;
    let mut line = 0_usize;
    let mut character = 0_usize;
    for current in text.chars() {
        if offset == target_offset {
            return Some((line, character));
        }
        offset += current.len_utf16();
        if current == '\n' {
            line += 1;
            character = 0;
        } else {
            character += current.len_utf16();
        }
    }
    (offset == target_offset).then_some((line, character))
}

fn utf16_to_byte_offset(text: &str, target_offset: usize) -> Result<usize, String> {
    let mut offset = 0_usize;
    for (byte, current) in text.char_indices() {
        if offset == target_offset {
            return Ok(byte);
        }
        offset += current.len_utf16();
    }
    if offset == target_offset {
        Ok(text.len())
    } else {
        Err(format!("UTF-16 offset {target_offset} is out of bounds"))
    }
}

fn resolve_code_lens(ide: &Ide, settings: &Value, mut lens: Value) -> Result<Value, String> {
    let Some(data) = lens.get("data").cloned() else {
        return Ok(lens);
    };
    let uri = data.get("uri").and_then(Value::as_str).unwrap_or_default();
    let line = data.get("line").and_then(Value::as_u64).unwrap_or(0);
    let character = data.get("character").and_then(Value::as_u64).unwrap_or(0);
    let references = ide.references(
        uri,
        TextPosition {
            line: line.try_into().unwrap_or(0),
            character: character.try_into().unwrap_or(0),
        },
        false,
    )?;
    let reference_count = references.as_array().map_or(0, Vec::len);
    let Some(object) = lens.as_object_mut() else {
        return Ok(lens);
    };
    object.insert(
        "command".to_string(),
        json!({
            "title": localize_code_lens_references(settings_locale(settings), reference_count),
            "command": "aspLsp.showReferences",
            "arguments": [
                uri,
                { "line": line, "character": character },
                references,
            ],
        }),
    );
    Ok(lens)
}

fn settings_locale(settings: &Value) -> &'static str {
    if settings.get("locale").and_then(Value::as_str) == Some("ja") {
        "ja"
    } else {
        "en"
    }
}

fn localize_code_lens_references(locale: &str, count: usize) -> String {
    if locale == "ja" {
        format!("{count} 件の参照")
    } else if count == 1 {
        "1 reference".to_string()
    } else {
        format!("{count} references")
    }
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

fn publish_backend_status(connection: &Connection, state: &ServerState) -> Result<(), String> {
    connection
        .sender
        .send(Notification::new(BACKEND_STATUS_METHOD.to_string(), state.backend_status()).into())
        .map_err(|error| error.to_string())
}

fn pointer_string(params: &Value, pointer: &str) -> String {
    params
        .pointer(pointer)
        .and_then(Value::as_str)
        .unwrap_or_default()
        .to_string()
}

fn request_text_document_uri(params: &Value) -> String {
    let text_document_uri = pointer_string(params, "/textDocument/uri");
    if text_document_uri.is_empty() {
        pointer_string(params, "/uri")
    } else {
        text_document_uri
    }
}

fn write_sidecar_frame(stdin: &mut ChildStdin, request: &EmbeddedRequest) -> Result<(), String> {
    let payload = serde_json::to_vec(request).map_err(|error| error.to_string())?;
    let length = payload
        .len()
        .checked_add(1)
        .ok_or_else(|| "embedded sidecar frame is too large".to_string())?;
    let length: u32 = length
        .try_into()
        .map_err(|_| "embedded sidecar frame is too large".to_string())?;
    stdin
        .write_all(&length.to_le_bytes())
        .and_then(|_| stdin.write_all(&[FRAME_KIND_JSON]))
        .and_then(|_| stdin.write_all(&payload))
        .and_then(|_| stdin.flush())
        .map_err(|error| error.to_string())
}

fn read_sidecar_frame(stdout: &mut ChildStdout) -> Result<EmbeddedResponse, String> {
    let mut header = [0; 4];
    stdout
        .read_exact(&mut header)
        .map_err(|error| format!("embedded sidecar response header failed: {error}"))?;
    let length = u32::from_le_bytes(header) as usize;
    if length == 0 {
        return Err("embedded sidecar returned an empty frame".to_string());
    }
    let mut frame = vec![0; length];
    stdout
        .read_exact(&mut frame)
        .map_err(|error| format!("embedded sidecar response body failed: {error}"))?;
    if frame[0] != FRAME_KIND_JSON {
        return Err(format!("unknown embedded sidecar frame kind: {}", frame[0]));
    }
    serde_json::from_slice(&frame[1..]).map_err(|error| error.to_string())
}

fn resolve_sidecar_path() -> Result<PathBuf, String> {
    if let Ok(path) = env::var("ASP_LSP_EMBEDDED_SIDECAR_PATH") {
        return Ok(PathBuf::from(path));
    }
    let exe = env::current_exe().map_err(|error| error.to_string())?;
    for candidate in sidecar_path_candidates(&exe) {
        if candidate.exists() {
            return Ok(candidate);
        }
    }
    Err("embedded sidecar dist/sidecar.js was not found".to_string())
}

fn sidecar_path_candidates(exe: &Path) -> Vec<PathBuf> {
    let mut candidates = Vec::new();
    if let Some(dir) = exe.parent() {
        candidates.push(
            dir.join("server")
                .join("sidecar")
                .join("dist")
                .join("sidecar.js"),
        );
        candidates.push(
            dir.join("..")
                .join("..")
                .join("sidecar")
                .join("dist")
                .join("sidecar.js"),
        );
        candidates.push(
            dir.join("..")
                .join("sidecar")
                .join("dist")
                .join("sidecar.js"),
        );
        candidates.push(
            dir.join("..")
                .join("..")
                .join("packages")
                .join("embedded-sidecar")
                .join("dist")
                .join("sidecar.js"),
        );
    }
    if let Ok(cwd) = env::current_dir() {
        candidates.push(
            cwd.join("packages")
                .join("embedded-sidecar")
                .join("dist")
                .join("sidecar.js"),
        );
        candidates.push(
            cwd.join("apps")
                .join("vscode")
                .join("server")
                .join("sidecar")
                .join("dist")
                .join("sidecar.js"),
        );
    }
    candidates
}
