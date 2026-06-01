use std::collections::HashMap;
use std::env;
use std::fs::{self, File};
use std::io::{Read, Write};
use std::path::{Path, PathBuf};
use std::process::{Child, ChildStdin, ChildStdout, Command, Stdio};
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use asp_ide::{Ide, MappedVirtualDocument, TextPosition, TextRange};
use asp_sidecar_protocol::{EmbeddedRequest, EmbeddedResponse, VirtualDocument};
use crossbeam_channel::RecvTimeoutError;
use lsp_server::{Connection, ErrorCode, Message, Notification, Request, Response};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};

const BACKEND_STATUS_METHOD: &str = "aspLsp/backendStatus";
const FRAME_KIND_JSON: u8 = 1;
const DISK_CACHE_FORMAT_VERSION: u32 = 3;
const DEFAULT_CACHE_TTL_HOURS: f64 = 24.0 * 14.0;
const DEFAULT_CACHE_MAX_SIZE_MB: f64 = 128.0;
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
    publish_backend_status(&connection, &state)?;

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
    sidecar: EmbeddedSidecar,
    disk_cache: DiskAnalysisCache,
    semantic_tokens: SemanticTokenCache,
    indexed_files: Vec<IndexedFile>,
    settings: Value,
    workspace_roots: Vec<String>,
    sidecar_request_id: u64,
}

impl ServerState {
    fn set_settings(&mut self, settings: Value) -> Result<Vec<(String, Vec<Value>)>, String> {
        self.diagnostics.set_debounce_from_settings(&settings);
        self.settings = settings.clone();
        self.configure_disk_cache();
        self.ide.set_settings(settings)
    }

    fn set_workspace_roots(&mut self, roots: Vec<String>) -> Result<(), String> {
        let indexed_files = index_workspace_files(&roots)?;
        self.ide.replace_indexed_documents(
            indexed_files
                .iter()
                .map(|file| (file.uri.clone(), file.text.clone()))
                .collect(),
        );
        self.indexed_files = indexed_files;
        self.workspace_roots = roots;
        self.configure_disk_cache();
        Ok(())
    }

    fn refresh_workspace_index(&mut self) -> Result<(), String> {
        self.set_workspace_roots(self.workspace_roots.clone())
    }

    fn execute_command(&mut self, command: &str) -> Result<Value, String> {
        match command {
            "aspLsp.server.reindexWorkspace" => {
                self.refresh_workspace_index()?;
            }
            "aspLsp.server.clearCache" => {
                self.disk_cache.clear()?;
                self.ide.clear_process_cache();
                self.semantic_tokens.clear_all();
            }
            "aspLsp.server.clearDiskCache" => {
                self.disk_cache.clear()?;
            }
            "aspLsp.server.clearProcessCache" => {
                self.ide.clear_process_cache();
                self.semantic_tokens.clear_all();
            }
            _ => {
                return Err(format!("unknown command: {command}"));
            }
        }
        Ok(json!({ "ok": true, "command": command }))
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
            let started_at = Instant::now();
            let diagnostics = self.full_diagnostics(&uri)?;
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

    fn configure_disk_cache(&mut self) {
        self.disk_cache = DiskAnalysisCache::from_settings(&self.settings, &self.workspace_roots);
        self.disk_cache.sweep();
    }

    fn publish_fast_diagnostics(&self, connection: &Connection, uri: &str) -> Result<(), String> {
        let diagnostics = self.ide.parser_diagnostics(uri)?;
        send_diagnostics(connection, uri, diagnostics)
    }

    fn full_diagnostics(&mut self, uri: &str) -> Result<Vec<Value>, String> {
        let mut diagnostics = self.ide.diagnostics(uri)?;
        diagnostics.extend(self.embedded_diagnostics(uri)?);
        Ok(diagnostics)
    }

    fn text_document_diagnostic(&mut self, uri: &str) -> Result<Value, String> {
        let diagnostics = self.full_diagnostics(uri)?;
        Ok(json!({
            "kind": "full",
            "items": diagnostics,
        }))
    }

    fn workspace_diagnostic(&mut self, connection: &Connection) -> Result<Value, String> {
        let mut reports = Vec::new();
        for uri in self.ide.open_document_uris() {
            reports.push(json!({
                "kind": "full",
                "uri": uri,
                "version": null,
                "items": self.full_diagnostics(&uri)?,
            }));
        }
        for file in self.indexed_files.clone() {
            if self.ide.is_open_document(&file.uri) {
                continue;
            }
            reports.push(json!({
                "kind": "full",
                "uri": file.uri,
                "version": null,
                "items": self.indexed_diagnostics(connection, &file)?,
            }));
        }
        Ok(json!({ "items": reports }))
    }

    fn indexed_diagnostics(
        &mut self,
        connection: &Connection,
        file: &IndexedFile,
    ) -> Result<Vec<Value>, String> {
        let lookup = DiskCacheLookup {
            source: file.source.clone(),
            settings_key: disk_analysis_settings_key(&self.settings),
        };
        if let Some(diagnostics) = self.disk_cache.read_analysis(&lookup) {
            log_debug_summary(
                connection,
                &self.settings,
                format!("[asp-lsp] diskCache.hit: {}", file.uri),
            )?;
            return Ok(diagnostics);
        }
        if self.disk_cache.enabled {
            log_debug_summary(
                connection,
                &self.settings,
                format!("[asp-lsp] diskCache.miss: {}", file.uri),
            )?;
        }
        let diagnostics = self.ide.workspace_diagnostics(&file.uri)?;
        self.disk_cache.write_analysis(&lookup, &diagnostics);
        if self.disk_cache.enabled {
            log_debug_summary(
                connection,
                &self.settings,
                format!("[asp-lsp] diskCache.write: {}", file.uri),
            )?;
        }
        Ok(diagnostics)
    }

    fn semantic_tokens_full(&mut self, uri: &str) -> Result<Value, String> {
        let value = self.ide.semantic_tokens(uri, None)?;
        Ok(self.semantic_tokens.full(uri, value))
    }

    fn semantic_tokens_delta(
        &mut self,
        uri: &str,
        previous_result_id: &str,
    ) -> Result<Value, String> {
        let value = self.ide.semantic_tokens(uri, None)?;
        Ok(self.semantic_tokens.delta(uri, previous_result_id, value))
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

    fn embedded_diagnostics(&mut self, uri: &str) -> Result<Vec<Value>, String> {
        let virtuals = self.ide.embedded_virtual_documents(uri)?;
        let open_virtuals = virtuals
            .iter()
            .map(|mapped| mapped.document.clone())
            .collect::<Vec<_>>();
        let mut diagnostics = Vec::new();
        for mapped in virtuals {
            if mapped.document.language_id == "vbscript" {
                continue;
            }
            let request_id = self.next_sidecar_request_id();
            let response = match self.sidecar.request(EmbeddedRequest {
                id: request_id,
                operation: "diagnostics".to_string(),
                active_virtual: mapped.document.clone(),
                open_virtuals: open_virtuals.clone(),
                settings: self.settings.clone(),
                workspace_roots: self.workspace_roots.clone(),
                project_generation: 0,
                params: Value::Null,
            }) {
                Ok(response) => response,
                Err(error) => {
                    if !error.contains("embedded sidecar dist/sidecar.js was not found") {
                        eprintln!("embedded sidecar diagnostics failed: {error}");
                    }
                    continue;
                }
            };
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

    fn embedded_position_feature(
        &mut self,
        uri: &str,
        operation: &str,
        position: TextPosition,
    ) -> Result<Option<Value>, String> {
        let Some((mapped, virtual_position)) =
            self.ide.embedded_virtual_document_at(uri, position)?
        else {
            return Ok(None);
        };
        let open_virtuals = self.open_virtual_documents(uri)?;
        let result = self.embedded_request(
            &mapped,
            open_virtuals,
            operation,
            json!({ "position": virtual_position }),
        )?;
        Ok(result.map(|value| mapped.remap_lsp_value(value)))
    }

    fn embedded_document_feature(&mut self, uri: &str, operation: &str) -> Result<Value, String> {
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
            let Some(result) =
                self.embedded_request(&mapped, open_virtuals.clone(), operation, Value::Null)?
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

    fn embedded_request(
        &mut self,
        mapped: &MappedVirtualDocument,
        open_virtuals: Vec<VirtualDocument>,
        operation: &str,
        params: Value,
    ) -> Result<Option<Value>, String> {
        let request_id = self.next_sidecar_request_id();
        let response = match self.sidecar.request(EmbeddedRequest {
            id: request_id,
            operation: operation.to_string(),
            active_virtual: mapped.document.clone(),
            open_virtuals,
            settings: self.settings.clone(),
            workspace_roots: self.workspace_roots.clone(),
            project_generation: 0,
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

struct DiskCacheLookup {
    source: DiskSourceMetadata,
    settings_key: String,
}

#[derive(Deserialize, Serialize)]
struct PersistedDiskAnalysisEntry {
    kind: String,
    #[serde(rename = "formatVersion")]
    format_version: u32,
    #[serde(rename = "toolVersion")]
    tool_version: String,
    namespace: String,
    #[serde(rename = "writtenAt")]
    written_at: f64,
    source: DiskSourceMetadata,
    #[serde(rename = "settingsKey")]
    settings_key: String,
    diagnostics: Vec<Value>,
}

struct DiskAnalysisCache {
    enabled: bool,
    root: PathBuf,
    ttl_ms: f64,
    max_size_bytes: u64,
    namespace: String,
    tool_version: String,
}

impl Default for DiskAnalysisCache {
    fn default() -> Self {
        Self::from_settings(&Value::Null, &[])
    }
}

impl DiskAnalysisCache {
    fn from_settings(settings: &Value, workspace_roots: &[String]) -> Self {
        let cache = settings.get("cache").unwrap_or(&Value::Null);
        let enabled = cache
            .get("enabled")
            .and_then(Value::as_bool)
            .unwrap_or(true);
        let root = cache
            .get("directory")
            .and_then(Value::as_str)
            .filter(|directory| !directory.trim().is_empty())
            .map(PathBuf::from)
            .unwrap_or_else(|| env::temp_dir().join("asp-lsp-analysis-cache"));
        let ttl_hours = cache
            .get("ttlHours")
            .and_then(Value::as_f64)
            .unwrap_or(DEFAULT_CACHE_TTL_HOURS)
            .max(0.000001);
        let max_size_mb = cache
            .get("maxSizeMb")
            .and_then(Value::as_f64)
            .unwrap_or(DEFAULT_CACHE_MAX_SIZE_MB)
            .max(0.000001);
        Self {
            enabled,
            root,
            ttl_ms: ttl_hours * 60.0 * 60.0 * 1000.0,
            max_size_bytes: (max_size_mb * 1024.0 * 1024.0) as u64,
            namespace: disk_analysis_namespace(workspace_roots),
            tool_version: language_server_version(),
        }
    }

    fn read_analysis(&self, lookup: &DiskCacheLookup) -> Option<Vec<Value>> {
        if !self.enabled {
            return None;
        }
        let path = self.file_name_for_lookup(lookup, "diagnostics");
        let entry: PersistedDiskAnalysisEntry = ciborium::de::from_reader(File::open(&path).ok()?)
            .inspect_err(|_| {
                let _ = fs::remove_file(&path);
            })
            .ok()?;
        if self.matches(&entry, lookup, "diagnostics") {
            Some(entry.diagnostics)
        } else {
            None
        }
    }

    fn write_analysis(&self, lookup: &DiskCacheLookup, diagnostics: &[Value]) {
        if !self.enabled {
            return;
        }
        if fs::create_dir_all(&self.root).is_err() {
            return;
        }
        let entry = PersistedDiskAnalysisEntry {
            kind: "diagnostics".to_string(),
            format_version: DISK_CACHE_FORMAT_VERSION,
            tool_version: self.tool_version.clone(),
            namespace: self.namespace.clone(),
            written_at: now_ms(),
            source: lookup.source.clone(),
            settings_key: lookup.settings_key.clone(),
            diagnostics: diagnostics.to_vec(),
        };
        let Ok(file) = File::create(self.file_name_for_lookup(lookup, "diagnostics")) else {
            return;
        };
        let _ = ciborium::ser::into_writer(&entry, file);
    }

    fn sweep(&self) {
        if !self.enabled {
            return;
        }
        let Ok(entries) = fs::read_dir(&self.root) else {
            return;
        };
        let now = now_ms();
        let mut live_files = Vec::new();
        for entry in entries.flatten() {
            let path = entry.path();
            if path.extension().and_then(|extension| extension.to_str()) != Some("cbor") {
                continue;
            }
            let size = entry.metadata().map(|metadata| metadata.len()).unwrap_or(0);
            let persisted: Option<PersistedDiskAnalysisEntry> = File::open(&path)
                .ok()
                .and_then(|file| ciborium::de::from_reader(file).ok());
            let Some(persisted) = persisted else {
                let _ = fs::remove_file(&path);
                continue;
            };
            if now - persisted.written_at > self.ttl_ms {
                let _ = fs::remove_file(&path);
                continue;
            }
            live_files.push((path, size, persisted.written_at));
        }
        let mut total = live_files.iter().map(|(_, size, _)| *size).sum::<u64>();
        live_files.sort_by(|left, right| {
            left.2
                .partial_cmp(&right.2)
                .unwrap_or(std::cmp::Ordering::Equal)
        });
        for (path, size, _) in live_files {
            if total <= self.max_size_bytes {
                break;
            }
            if fs::remove_file(path).is_ok() {
                total = total.saturating_sub(size);
            }
        }
    }

    fn clear(&self) -> Result<(), String> {
        let Ok(entries) = fs::read_dir(&self.root) else {
            return Ok(());
        };
        for entry in entries.flatten() {
            let path = entry.path();
            if path.extension().and_then(|extension| extension.to_str()) == Some("cbor") {
                fs::remove_file(&path).map_err(|error| {
                    format!(
                        "failed to remove disk cache entry {}: {error}",
                        path.display()
                    )
                })?;
            }
        }
        Ok(())
    }

    fn matches(
        &self,
        entry: &PersistedDiskAnalysisEntry,
        lookup: &DiskCacheLookup,
        kind: &str,
    ) -> bool {
        entry.kind == kind
            && entry.format_version == DISK_CACHE_FORMAT_VERSION
            && entry.tool_version == self.tool_version
            && entry.namespace == self.namespace
            && entry.settings_key == lookup.settings_key
            && entry.source.file_name == lookup.source.file_name
            && entry.source.mtime_ms == lookup.source.mtime_ms
            && entry.source.size == lookup.source.size
            && now_ms() - entry.written_at <= self.ttl_ms
    }

    fn file_name_for_lookup(&self, lookup: &DiskCacheLookup, kind: &str) -> PathBuf {
        self.root.join(format!(
            "{}.cbor",
            stable_hash(
                &json!({
                    "kind": kind,
                    "namespace": self.namespace,
                    "fileName": lookup.source.file_name,
                    "settingsKey": lookup.settings_key,
                })
                .to_string()
            )
        ))
    }
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
}

impl SemanticTokenCache {
    fn full(&mut self, uri: &str, mut value: Value) -> Value {
        let data = semantic_token_data(&value);
        let result_id = self.store(uri, data);
        if let Some(object) = value.as_object_mut() {
            object.insert("resultId".to_string(), Value::String(result_id));
        }
        value
    }

    fn delta(&mut self, uri: &str, previous_result_id: &str, value: Value) -> Value {
        let next = semantic_token_data(&value);
        let previous = self
            .results
            .get(previous_result_id)
            .filter(|result| result.uri == uri)
            .map(|result| result.data.as_slice());
        let edit = previous
            .map(|previous| semantic_token_delta_edit(previous, &next))
            .unwrap_or_else(|| json!({ "start": 0, "deleteCount": 0, "data": next.clone() }));
        let result_id = self.store(uri, next);
        json!({
            "resultId": result_id,
            "edits": [edit],
        })
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

    fn store(&mut self, uri: &str, data: Vec<Value>) -> String {
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
            },
        );
        result_id
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

struct EmbeddedSidecar {
    process: Option<EmbeddedSidecarProcess>,
}

impl Default for EmbeddedSidecar {
    fn default() -> Self {
        Self { process: None }
    }
}

impl EmbeddedSidecar {
    fn is_running(&self) -> bool {
        self.process.is_some()
    }

    fn request(&mut self, request: EmbeddedRequest) -> Result<EmbeddedResponse, String> {
        self.request_inner(&request).or_else(|error| {
            self.process = None;
            self.request_inner(&request)
                .map_err(|retry_error| format!("{error}; retry failed: {retry_error}"))
        })
    }

    fn request_inner(&mut self, request: &EmbeddedRequest) -> Result<EmbeddedResponse, String> {
        if self.process.is_none() {
            self.process = Some(EmbeddedSidecarProcess::start()?);
        }
        let response = self
            .process
            .as_mut()
            .ok_or_else(|| "embedded sidecar was not started".to_string())?
            .request(request);
        match response {
            Ok(response) => {
                if response.ok {
                    Ok(response)
                } else {
                    Err(response
                        .error
                        .unwrap_or_else(|| "embedded sidecar failed".to_string()))
                }
            }
            Err(error) => {
                self.process = None;
                Err(error)
            }
        }
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
        "textDocument/completion" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let position = request_position(&request.params)?;
            let result = merge_lsp_arrays(
                state.ide.completion(&uri, position)?,
                state.embedded_position_feature(&uri, "completion", position)?,
            );
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "completionItem/resolve" => {
            connection
                .sender
                .send(Response::new_ok(request.id, request.params).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/hover" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let position = request_position(&request.params)?;
            let result = state.ide.hover(&uri, position)?;
            let result = if result.is_null() {
                state
                    .embedded_position_feature(&uri, "hover", position)?
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
        "textDocument/definition"
        | "textDocument/declaration"
        | "textDocument/typeDefinition"
        | "textDocument/implementation" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let position = request_position(&request.params)?;
            let result = state.ide.definition(&uri, position)?;
            let result = if result.is_null() {
                state
                    .embedded_position_feature(&uri, "definition", position)?
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
                Some(state.embedded_document_feature(&uri, "documentSymbols")?),
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
                Some(state.embedded_document_feature(&uri, "foldingRanges")?),
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
            let result = state.ide.document_highlights(&uri, position)?;
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
            let result = state.ide.prepare_rename(&uri, position)?;
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
            let result = state.ide.rename(&uri, position, &new_name)?;
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
            let result = state.ide.semantic_tokens(&uri, Some(range))?;
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/documentColor" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let result = state.embedded_document_feature(&uri, "documentColors")?;
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
        "workspace/diagnostic" => {
            let result = state.workspace_diagnostic(connection)?;
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
            let result = resolve_code_lens(&state.ide, request.params)?;
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/selectionRange" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let positions = request_positions(&request.params)?;
            let result = state.ide.selection_ranges(&uri, &positions)?;
            connection
                .sender
                .send(Response::new_ok(request.id, result).into())
                .map_err(|error| error.to_string())?;
            Ok(false)
        }
        "textDocument/inlayHint" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let range = request_range(&request.params)?;
            let result = state.ide.inlay_hints(&uri, range)?;
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
        "textDocument/prepareCallHierarchy" | "textDocument/prepareTypeHierarchy" => {
            let uri = pointer_string(&request.params, "/textDocument/uri");
            let position = request_position(&request.params)?;
            let result = state.ide.hierarchy_item(&uri, position)?;
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
            let result = state
                .ide
                .formatting_edits(&uri, range, options, &state.settings)?;
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
                .embedded_position_feature(&uri, "linkedEditingRanges", position)?
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
        "diagnosticProvider": {
            "interFileDependencies": true,
            "workspaceDiagnostics": true,
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
                "aspLsp.server.clearDiskCache",
                "aspLsp.server.clearProcessCache",
            ],
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

fn index_workspace_files(roots: &[String]) -> Result<Vec<IndexedFile>, String> {
    let mut files = Vec::new();
    for root in roots {
        let Some(path) = file_uri_to_path(root) else {
            continue;
        };
        collect_workspace_files(&path, &mut files)?;
        if files.len() >= 512 {
            break;
        }
    }
    Ok(files)
}

fn collect_workspace_files(path: &Path, files: &mut Vec<IndexedFile>) -> Result<(), String> {
    if files.len() >= 512 {
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
        collect_workspace_files(&entry.path(), files)?;
        if files.len() >= 512 {
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

fn disk_analysis_namespace(workspace_roots: &[String]) -> String {
    let mut roots = workspace_roots.to_vec();
    roots.sort();
    stable_hash(
        &json!({
            "roots": roots,
            "cwd": env::current_dir()
                .ok()
                .map(|path| normalize_file_name(&path))
                .unwrap_or_default(),
        })
        .to_string(),
    )
}

fn disk_analysis_settings_key(settings: &Value) -> String {
    json!({
        "rust": 1,
        "settings": settings,
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

fn now_ms() -> f64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|duration| duration.as_secs_f64() * 1000.0)
        .unwrap_or(0.0)
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

fn format_on_save_enabled(settings: &Value) -> bool {
    settings
        .get("format")
        .and_then(|format| format.get("onSave"))
        .and_then(Value::as_bool)
        .unwrap_or(false)
}

fn log_debug_summary(
    connection: &Connection,
    settings: &Value,
    message: String,
) -> Result<(), String> {
    if settings
        .get("debug")
        .and_then(|debug| debug.get("output"))
        .and_then(Value::as_str)
        != Some("verbose")
    {
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

fn resolve_code_lens(ide: &Ide, mut lens: Value) -> Result<Value, String> {
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
            "title": format!("{reference_count} references"),
            "command": "aspLsp.showReferences",
            "arguments": [
                uri,
                { "line": line, "character": character },
                references,
            ],
        }),
    );
    if reference_count == 1 {
        if let Some(command) = lens.pointer_mut("/command/title") {
            *command = Value::String("1 reference".to_string());
        }
    }
    Ok(lens)
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
