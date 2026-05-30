use serde_json::{json, Map, Value};
use std::collections::{HashMap, HashSet};
use std::sync::OnceLock;

type JsonMap = Map<String, Value>;
const DAEMON_CACHE_ENTRY_LIMIT: usize = 4096;

pub fn handle_json(input: &str) -> Result<String, String> {
    let request: Value = serde_json::from_str(input).map_err(|error| error.to_string())?;
    let result = CoreState::default().handle_value(&request)?;
    serde_json::to_string(&result).map_err(|error| error.to_string())
}

pub fn handle_value(request: &Value) -> Result<Value, String> {
    CoreState::default().handle_value(request)
}

#[derive(Default)]
pub struct CoreState {
    parsed_cache: HashMap<String, Value>,
    symbol_cache: HashMap<String, Vec<Value>>,
    diagnostics_cache: HashMap<String, Vec<Value>>,
    serialized_result_cache: HashMap<String, String>,
}

impl CoreState {
    pub fn handle_serialized_value(&mut self, request: &Value) -> Result<String, String> {
        let serialized_key = serialized_result_cache_key(request);
        if let Some(cache_key) = &serialized_key {
            if let Some(result) = self.serialized_result_cache.get(cache_key) {
                return Ok(result.clone());
            }
        }
        let result = self.handle_value(request)?;
        let serialized = serde_json::to_string(&result).map_err(|error| error.to_string())?;
        if let Some(cache_key) = serialized_key {
            if self.serialized_result_cache.len() >= DAEMON_CACHE_ENTRY_LIMIT {
                self.serialized_result_cache.clear();
            }
            self.serialized_result_cache
                .insert(cache_key, serialized.clone());
        }
        Ok(serialized)
    }

    pub fn handle_vbscript_columnar_frame_body(
        &mut self,
        id: u32,
        request: &Value,
    ) -> Result<Vec<u8>, String> {
        let operation = request
            .get("operation")
            .and_then(Value::as_str)
            .ok_or_else(|| "operation is required".to_string())?;
        if operation != "parseAspDocumentVbscript" {
            return Err(format!("unsupported columnar operation: {operation}"));
        }
        let uri = request
            .get("uri")
            .and_then(Value::as_str)
            .unwrap_or_default();
        let text = request
            .get("text")
            .and_then(Value::as_str)
            .unwrap_or_default();
        let settings = request.get("settings").unwrap_or(&Value::Null);
        let parsed = self.cached_parse_asp_document(request, uri, text, settings);
        encode_document_vbscript_columnar_frame_body(id, &parsed)
    }

    pub fn handle_value(&mut self, request: &Value) -> Result<Value, String> {
        let operation = request
            .get("operation")
            .and_then(Value::as_str)
            .ok_or_else(|| "operation is required".to_string())?;
        let result = match operation {
            "backendInfo" => json!({
                "backend": "native",
                "engine": "asp-lsp-core",
                "version": env!("CARGO_PKG_VERSION"),
            }),
            "parseVbscriptCst" => {
                let text = request
                    .get("text")
                    .and_then(Value::as_str)
                    .unwrap_or_default();
                let source_text = request
                    .get("sourceText")
                    .and_then(Value::as_str)
                    .unwrap_or(text);
                let base_offset = request
                    .get("baseOffset")
                    .and_then(Value::as_u64)
                    .unwrap_or(0) as usize;
                parse_vbscript_cst(text, source_text, base_offset)
            }
            "parseAspCst" => {
                let text = request
                    .get("text")
                    .and_then(Value::as_str)
                    .unwrap_or_default();
                let settings = request.get("settings").unwrap_or(&Value::Null);
                parse_asp_cst(text, settings)
            }
            "parseAspDocument" => {
                let uri = request
                    .get("uri")
                    .and_then(Value::as_str)
                    .unwrap_or_default();
                let text = request
                    .get("text")
                    .and_then(Value::as_str)
                    .unwrap_or_default();
                let settings = request.get("settings").unwrap_or(&Value::Null);
                self.cached_parse_asp_document(request, uri, text, settings)
            }
            "parseAspDocumentLight" => {
                let uri = request
                    .get("uri")
                    .and_then(Value::as_str)
                    .unwrap_or_default();
                let text = request
                    .get("text")
                    .and_then(Value::as_str)
                    .unwrap_or_default();
                let settings = request.get("settings").unwrap_or(&Value::Null);
                let parsed = self.cached_parse_asp_document(request, uri, text, settings);
                json!({
                    "uri": parsed.get("uri").cloned().unwrap_or(Value::Null),
                    "defaultLanguage": parsed.get("defaultLanguage").cloned().unwrap_or(Value::Null),
                    "regionCount": parsed.get("regions").and_then(Value::as_array).map_or(0, Vec::len),
                    "includeCount": parsed.get("includes").and_then(Value::as_array).map_or(0, Vec::len),
                    "diagnosticCount": parsed.get("diagnostics").and_then(Value::as_array).map_or(0, Vec::len),
                    "serverObjectCount": parsed.get("serverObjects").and_then(Value::as_array).map_or(0, Vec::len),
                })
            }
            "parseAspDocumentShallow" => {
                // 浅いドキュメント（CST スケルトン）を返す。重い VB CST/トークンは
                // parseAspDocumentVbscript で必要時に取得する（境界転送コスト削減のため）。
                let uri = request
                    .get("uri")
                    .and_then(Value::as_str)
                    .unwrap_or_default();
                let text = request
                    .get("text")
                    .and_then(Value::as_str)
                    .unwrap_or_default();
                let settings = request.get("settings").unwrap_or(&Value::Null);
                let parsed = self.cached_parse_asp_document(request, uri, text, settings);
                shallow_document(&parsed)
            }
            "parseAspDocumentVbscript" => {
                // 各 CST ノードに付く VB CST サブツリーを node.start で索引付けして返す。
                // TS 側はこれを浅い CST に attach して full 相当に復元する。
                let uri = request
                    .get("uri")
                    .and_then(Value::as_str)
                    .unwrap_or_default();
                let text = request
                    .get("text")
                    .and_then(Value::as_str)
                    .unwrap_or_default();
                let settings = request.get("settings").unwrap_or(&Value::Null);
                let parsed = self.cached_parse_asp_document(request, uri, text, settings);
                document_vbscript_segments(&parsed)
            }
            "collectVbscriptSymbols" => {
                let parsed = request
                    .get("parsed")
                    .ok_or_else(|| "parsed is required".to_string())?;
                let context = request.get("context").unwrap_or(&Value::Null);
                let symbols = self.cached_collect_symbols(request, parsed, context);
                Value::Array(symbols)
            }
            "collectVbscriptSymbolsFromText" => {
                let uri = request
                    .get("uri")
                    .and_then(Value::as_str)
                    .unwrap_or_default();
                let text = request
                    .get("text")
                    .and_then(Value::as_str)
                    .unwrap_or_default();
                let settings = request.get("settings").unwrap_or(&Value::Null);
                let context = request.get("context").unwrap_or(&Value::Null);
                let parsed = self.cached_parse_asp_document(request, uri, text, settings);
                let symbols = self.cached_collect_symbols(request, &parsed, context);
                Value::Array(symbols)
            }
            "summarizeAspFileAnalysis" => {
                let parsed = request
                    .get("parsed")
                    .ok_or_else(|| "parsed is required".to_string())?;
                summarize_asp_file(parsed)
            }
            "summarizeAspFileAnalysisFromText" => {
                let uri = request
                    .get("uri")
                    .and_then(Value::as_str)
                    .unwrap_or_default();
                let text = request
                    .get("text")
                    .and_then(Value::as_str)
                    .unwrap_or_default();
                let settings = request.get("settings").unwrap_or(&Value::Null);
                let parsed = self.cached_parse_asp_document(request, uri, text, settings);
                summarize_asp_file(&parsed)
            }
            "analyzeVbscript" => {
                let parsed = request
                    .get("parsed")
                    .ok_or_else(|| "parsed is required".to_string())?;
                let context = request.get("context").unwrap_or(&Value::Null);
                let symbols = self.cached_collect_symbols(request, parsed, context);
                let diagnostics = self.cached_diagnostics(request, parsed, &symbols, context);
                json!({ "diagnostics": diagnostics, "symbols": symbols })
            }
            "analyzeVbscriptFromText" => {
                let uri = request
                    .get("uri")
                    .and_then(Value::as_str)
                    .unwrap_or_default();
                let text = request
                    .get("text")
                    .and_then(Value::as_str)
                    .unwrap_or_default();
                let settings = request.get("settings").unwrap_or(&Value::Null);
                let context = request.get("context").unwrap_or(&Value::Null);
                let parsed = self.cached_parse_asp_document(request, uri, text, settings);
                let symbols = self.cached_collect_symbols(request, &parsed, context);
                let diagnostics = self.cached_diagnostics(request, &parsed, &symbols, context);
                json!({ "diagnostics": diagnostics, "symbols": symbols })
            }
            _ => return Err(format!("unknown operation: {operation}")),
        };
        Ok(result)
    }

    fn cached_parse_asp_document(
        &mut self,
        request: &Value,
        uri: &str,
        text: &str,
        settings: &Value,
    ) -> Value {
        let Some(cache_key) = request.get("cacheKey").and_then(Value::as_str) else {
            return parse_asp_document(uri, text, settings);
        };
        if !self.parsed_cache.contains_key(cache_key) {
            if self.parsed_cache.len() >= DAEMON_CACHE_ENTRY_LIMIT {
                self.clear_caches();
            }
            self.parsed_cache.insert(
                cache_key.to_string(),
                parse_asp_document(uri, text, settings),
            );
        }
        self.parsed_cache
            .get(cache_key)
            .cloned()
            .unwrap_or_else(|| parse_asp_document(uri, text, settings))
    }

    fn cached_collect_symbols(
        &mut self,
        request: &Value,
        parsed: &Value,
        context: &Value,
    ) -> Vec<Value> {
        let Some(cache_key) = semantic_cache_key(request, context, "symbols") else {
            return collect_symbols_from_parsed(parsed, context);
        };
        if let Some(symbols) = self.symbol_cache.get(&cache_key) {
            return symbols.clone();
        }
        let symbols = collect_symbols_from_parsed(parsed, context);
        if self.symbol_cache.len() >= DAEMON_CACHE_ENTRY_LIMIT {
            self.symbol_cache.clear();
            self.diagnostics_cache.clear();
        }
        self.symbol_cache.insert(cache_key, symbols.clone());
        symbols
    }

    fn cached_diagnostics(
        &mut self,
        request: &Value,
        parsed: &Value,
        symbols: &[Value],
        context: &Value,
    ) -> Vec<Value> {
        let Some(cache_key) = semantic_cache_key(request, context, "diagnostics") else {
            return diagnose_vbscript(parsed, symbols, context);
        };
        if let Some(diagnostics) = self.diagnostics_cache.get(&cache_key) {
            return diagnostics.clone();
        }
        let diagnostics = diagnose_vbscript(parsed, symbols, context);
        if self.diagnostics_cache.len() >= DAEMON_CACHE_ENTRY_LIMIT {
            self.diagnostics_cache.clear();
        }
        self.diagnostics_cache
            .insert(cache_key, diagnostics.clone());
        diagnostics
    }

    fn clear_caches(&mut self) {
        self.parsed_cache.clear();
        self.symbol_cache.clear();
        self.diagnostics_cache.clear();
        self.serialized_result_cache.clear();
    }
}

fn semantic_cache_key(request: &Value, context: &Value, operation: &str) -> Option<String> {
    let document_key = request.get("cacheKey").and_then(Value::as_str)?;
    let context_key = match context {
        Value::Null => String::new(),
        Value::Object(object) if object.is_empty() => String::new(),
        _ => {
            let serialized = serde_json::to_string(context).ok()?;
            if serialized.len() > 4096 {
                return None;
            }
            serialized
        }
    };
    Some(format!("{document_key}\0{operation}\0{context_key}"))
}

fn serialized_result_cache_key(request: &Value) -> Option<String> {
    let operation = request.get("operation").and_then(Value::as_str)?;
    match operation {
        "parseAspDocument"
        | "parseAspDocumentLight"
        | "parseAspDocumentShallow"
        | "parseAspDocumentVbscript" => {
            let document_key = request.get("cacheKey").and_then(Value::as_str)?;
            Some(format!("{document_key}\0{operation}"))
        }
        "collectVbscriptSymbolsFromText"
        | "analyzeVbscriptFromText"
        | "summarizeAspFileAnalysisFromText" => semantic_cache_key(
            request,
            request.get("context").unwrap_or(&Value::Null),
            operation,
        ),
        _ => None,
    }
}

#[no_mangle]
pub extern "C" fn asp_lsp_alloc(len: usize) -> *mut u8 {
    let mut buffer = Vec::<u8>::with_capacity(len);
    let pointer = buffer.as_mut_ptr();
    std::mem::forget(buffer);
    pointer
}

#[no_mangle]
pub unsafe extern "C" fn asp_lsp_dealloc(pointer: *mut u8, len: usize) {
    if !pointer.is_null() {
        let _ = Vec::from_raw_parts(pointer, len, len);
    }
}

static mut LAST_OUTPUT_LEN: usize = 0;

#[no_mangle]
pub unsafe extern "C" fn asp_lsp_handle(pointer: *const u8, len: usize) -> *mut u8 {
    let bytes = std::slice::from_raw_parts(pointer, len);
    let input = std::str::from_utf8(bytes).unwrap_or("{}");
    let output = handle_json(input).unwrap_or_else(|error| {
        serde_json::to_string(&json!({ "error": error })).unwrap_or_else(|_| "{}".to_string())
    });
    let output_bytes = output.as_bytes();
    let out = asp_lsp_alloc(output_bytes.len());
    std::ptr::copy_nonoverlapping(output_bytes.as_ptr(), out, output_bytes.len());
    LAST_OUTPUT_LEN = output_bytes.len();
    out
}

#[no_mangle]
pub unsafe extern "C" fn asp_lsp_last_output_len() -> usize {
    LAST_OUTPUT_LEN
}

#[derive(Clone)]
struct TextIndex<'a> {
    text: &'a str,
    utf16_to_byte: Vec<usize>,
    line_starts: Vec<usize>,
}

impl<'a> TextIndex<'a> {
    fn new(text: &'a str) -> Self {
        let mut utf16_to_byte = Vec::with_capacity(text.len() + 1);
        let mut line_starts = vec![0];
        let mut utf16 = 0;
        for (byte, ch) in text.char_indices() {
            utf16_to_byte.push(byte);
            if ch.len_utf16() == 2 {
                utf16_to_byte.push(byte);
            }
            utf16 += ch.len_utf16();
            if ch == '\n' {
                line_starts.push(utf16);
            }
        }
        utf16_to_byte.push(text.len());
        Self {
            text,
            utf16_to_byte,
            line_starts,
        }
    }

    fn len(&self) -> usize {
        self.utf16_to_byte.len().saturating_sub(1)
    }

    fn byte_at(&self, offset: usize) -> usize {
        self.utf16_to_byte
            .get(offset.min(self.len()))
            .copied()
            .unwrap_or(self.text.len())
    }

    fn slice(&self, start: usize, end: usize) -> &'a str {
        let start_byte = self.byte_at(start);
        let end_byte = self.byte_at(end.max(start));
        &self.text[start_byte..end_byte]
    }

    fn starts_with(&self, offset: usize, needle: &str) -> bool {
        self.slice(offset, self.len()).starts_with(needle)
    }

    fn char_at(&self, offset: usize) -> Option<char> {
        self.slice(offset, self.len()).chars().next()
    }

    fn position_at(&self, offset: usize) -> Value {
        let safe = offset.min(self.len());
        let mut low = 0usize;
        let mut high = self.line_starts.len().saturating_sub(1);
        let mut line = 0usize;
        while low <= high {
            let middle = (low + high) / 2;
            if self.line_starts[middle] <= safe {
                line = middle;
                low = middle + 1;
            } else if middle == 0 {
                break;
            } else {
                high = middle - 1;
            }
        }
        json!({ "line": line, "character": safe.saturating_sub(self.line_starts[line]) })
    }

    fn range(&self, start: usize, end: usize) -> Value {
        json!({ "start": self.position_at(start), "end": self.position_at(end) })
    }
}

fn parse_asp_document(uri: &str, text: &str, settings: &Value) -> Value {
    let cst = parse_asp_cst(text, settings);
    let diagnostics = cst
        .get("errors")
        .and_then(Value::as_array)
        .map(|errors| {
            errors
                .iter()
                .map(|error| {
                    json!({
                        "severity": 1,
                        "range": range_from_error(text, error),
                        "message": error.get("message").and_then(Value::as_str).unwrap_or_default(),
                        "source": "asp-lsp",
                    })
                })
                .collect::<Vec<_>>()
        })
        .unwrap_or_default();
    let children = cst
        .get("children")
        .and_then(Value::as_array)
        .cloned()
        .unwrap_or_default();
    let regions = children
        .iter()
        .filter_map(region_from_node)
        .collect::<Vec<_>>();
    let directives = children
        .iter()
        .filter_map(|node| node.get("directive").cloned())
        .collect::<Vec<_>>();
    let includes = children
        .iter()
        .filter_map(|node| node.get("include").cloned())
        .collect::<Vec<_>>();
    let server_objects = cst
        .get("serverObjects")
        .and_then(Value::as_array)
        .cloned()
        .unwrap_or_default();
    let default_language = default_language_from_directives(&directives, settings);
    json!({
        "uri": uri,
        "text": text,
        "cst": cst,
        "regions": regions,
        "directives": directives,
        "includes": includes,
        "serverObjects": server_objects,
        "defaultLanguage": default_language,
        "diagnostics": diagnostics,
    })
}

/// 浅いドキュメント投影。CST から重い `vbscript` サブツリーと `tokens` を取り除き、
/// 構造（kind/span/regionKind/language/children）だけ残す。regions/directives/includes
/// などのトップレベル情報はそのまま。
fn shallow_document(full: &Value) -> Value {
    let mut shallow = full.clone();
    // ドキュメント全文は TS 側が入力として保持しているため転送しない（受信側で再注入）。
    if let Some(object) = shallow.as_object_mut() {
        object.remove("text");
    }
    if let Some(cst) = shallow.get_mut("cst") {
        strip_cst_heavy(cst);
    }
    shallow
}

fn strip_cst_heavy(node: &mut Value) {
    if let Some(object) = node.as_object_mut() {
        object.remove("vbscript");
        // ノード単位の text 断片はソースから start/end で復元可能なため転送しない。
        object.remove("text");
        if let Some(tokens) = object.get_mut("tokens") {
            *tokens = Value::Array(Vec::new());
        }
        if let Some(Value::Array(children)) = object.get_mut("children") {
            for child in children.iter_mut() {
                strip_cst_heavy(child);
            }
        }
    }
}

/// CST 内の `vbscript` サブツリーを node.start で索引付けして列挙する。
/// 戻り値: `[{ "start": <usize>, "vbscript": <VbCstNode> }, ...]`
fn document_vbscript_segments(full: &Value) -> Value {
    let mut segments = Vec::new();
    if let Some(cst) = full.get("cst") {
        collect_vbscript_segments(cst, &mut segments);
    }
    Value::Array(segments)
}

fn collect_vbscript_segments(node: &Value, out: &mut Vec<Value>) {
    if let Some(object) = node.as_object() {
        if let Some(vbscript) = object.get("vbscript") {
            out.push(json!({
                "start": object.get("start").cloned().unwrap_or(Value::Null),
                "vbscript": vbscript.clone(),
            }));
        }
        if let Some(Value::Array(children)) = object.get("children") {
            for child in children {
                collect_vbscript_segments(child, out);
            }
        }
    }
}

fn encode_document_vbscript_columnar_frame_body(id: u32, full: &Value) -> Result<Vec<u8>, String> {
    let mut body = vec![1u8];
    write_u32(&mut body, id);
    let segments = document_vbscript_segments(full);
    let segments = segments
        .as_array()
        .ok_or_else(|| "vbscript segments must be an array".to_string())?;
    write_u32(&mut body, usize_to_u32(segments.len(), "segmentCount")?);
    for segment in segments {
        let node_start = value_to_u32(
            segment.get("start").unwrap_or(&Value::Null),
            "segment nodeStart",
        )?;
        let vbscript = segment
            .get("vbscript")
            .ok_or_else(|| "segment vbscript is required".to_string())?;
        let mut pool = ColumnarTokenPool::default();
        let tree = encode_vb_node_columnar(vbscript, &mut pool)?;
        write_u32(&mut body, node_start);
        write_u32(&mut body, usize_to_u32(pool.kind_codes.len(), "poolCount")?);
        body.extend_from_slice(&pool.kind_codes);
        for start in &pool.starts {
            write_u32(&mut body, *start);
        }
        for end in &pool.ends {
            write_u32(&mut body, *end);
        }
        write_u32(&mut body, usize_to_u32(pool.values.len(), "valueCount")?);
        for (token_index, value) in &pool.values {
            write_u32(&mut body, *token_index);
            let bytes = value.as_bytes();
            write_u32(&mut body, usize_to_u32(bytes.len(), "token value byteLen")?);
            body.extend_from_slice(bytes);
        }
        let tree_json = serde_json::to_vec(&tree).map_err(|error| error.to_string())?;
        write_u32(&mut body, usize_to_u32(tree_json.len(), "treeJsonLen")?);
        body.extend_from_slice(&tree_json);
    }
    Ok(body)
}

#[derive(Default)]
struct ColumnarTokenPool {
    kind_codes: Vec<u8>,
    starts: Vec<u32>,
    ends: Vec<u32>,
    values: Vec<(u32, String)>,
}

impl ColumnarTokenPool {
    fn append(&mut self, token: &Value) -> Result<u32, String> {
        let index = usize_to_u32(self.kind_codes.len(), "tokenIndex")?;
        let kind = token
            .get("kind")
            .and_then(Value::as_str)
            .unwrap_or("unknown");
        self.kind_codes.push(vb_token_kind_code(kind));
        self.starts.push(value_to_u32(
            token.get("start").unwrap_or(&Value::Null),
            "token start",
        )?);
        self.ends.push(value_to_u32(
            token.get("end").unwrap_or(&Value::Null),
            "token end",
        )?);
        if let Some(value) = token.get("value").and_then(Value::as_str) {
            self.values.push((index, value.to_string()));
        }
        Ok(index)
    }
}

fn encode_vb_node_columnar(node: &Value, pool: &mut ColumnarTokenPool) -> Result<Value, String> {
    let object = node
        .as_object()
        .ok_or_else(|| "VB CST node must be an object".to_string())?;
    let mut encoded = JsonMap::new();
    encoded.insert(
        "kind".to_string(),
        Value::String(
            object
                .get("kind")
                .and_then(Value::as_str)
                .unwrap_or_default()
                .to_string(),
        ),
    );
    encoded.insert(
        "start".to_string(),
        json!(value_to_u32(
            object.get("start").unwrap_or(&Value::Null),
            "node start"
        )?),
    );
    encoded.insert(
        "end".to_string(),
        json!(value_to_u32(
            object.get("end").unwrap_or(&Value::Null),
            "node end"
        )?),
    );
    insert_optional_u32_from_value(&mut encoded, object, "contentStart")?;
    insert_optional_u32_from_value(&mut encoded, object, "contentEnd")?;

    let token_lo = usize_to_u32(pool.kind_codes.len(), "tokens lo")?;
    if let Some(tokens) = object.get("tokens").and_then(Value::as_array) {
        for token in tokens {
            pool.append(token)?;
        }
    }
    let token_hi = usize_to_u32(pool.kind_codes.len(), "tokens hi")?;
    if let Some(token) = object.get("nameToken") {
        encoded.insert("nameToken".to_string(), json!(pool.append(token)?));
    }
    encoded.insert("tokens".to_string(), json!([token_lo, token_hi]));

    let identifiers = append_token_array(object, pool, "identifiers")?;
    if !identifiers.is_empty() {
        encoded.insert(
            "identifiers".to_string(),
            Value::Array(identifiers.into_iter().map(|index| json!(index)).collect()),
        );
    }
    let parameters = append_token_array(object, pool, "parameters")?;
    if !parameters.is_empty() {
        encoded.insert(
            "parameters".to_string(),
            Value::Array(parameters.into_iter().map(|index| json!(index)).collect()),
        );
    }

    let children = object
        .get("children")
        .and_then(Value::as_array)
        .map(|children| {
            children
                .iter()
                .map(|child| encode_vb_node_columnar(child, pool))
                .collect::<Result<Vec<_>, _>>()
        })
        .transpose()?
        .unwrap_or_default();
    encoded.insert("children".to_string(), Value::Array(children));

    for key in [
        "procedureKind",
        "propertyAccessor",
        "declarationKind",
        "visibility",
        "typeName",
        "memberOf",
        "scopeName",
    ] {
        if let Some(value) = object.get(key).and_then(Value::as_str) {
            encoded.insert(key.to_string(), Value::String(value.to_string()));
        }
    }
    insert_optional_u32_from_value(&mut encoded, object, "scopeStart")?;
    insert_optional_u32_from_value(&mut encoded, object, "scopeEnd")?;
    for key in ["arrayDeclarations", "parameterMetadata"] {
        if object
            .get(key)
            .and_then(Value::as_array)
            .is_some_and(|items| !items.is_empty())
        {
            encoded.insert(
                key.to_string(),
                object.get(key).cloned().unwrap_or(Value::Null),
            );
        }
    }
    Ok(Value::Object(encoded))
}

fn append_token_array(
    object: &JsonMap,
    pool: &mut ColumnarTokenPool,
    key: &str,
) -> Result<Vec<u32>, String> {
    let Some(tokens) = object.get(key).and_then(Value::as_array) else {
        return Ok(Vec::new());
    };
    tokens.iter().map(|token| pool.append(token)).collect()
}

fn insert_optional_u32_from_value(
    out: &mut JsonMap,
    object: &JsonMap,
    key: &str,
) -> Result<(), String> {
    if let Some(value) = object.get(key) {
        out.insert(key.to_string(), json!(value_to_u32(value, key)?));
    }
    Ok(())
}

fn vb_token_kind_code(kind: &str) -> u8 {
    match kind {
        "identifier" => 0,
        "keyword" => 1,
        "string" => 2,
        "number" => 3,
        "symbol" => 4,
        "comment" => 5,
        "whitespace" => 6,
        "newline" => 7,
        _ => 8,
    }
}

fn value_to_u32(value: &Value, label: &str) -> Result<u32, String> {
    let raw = value
        .as_u64()
        .ok_or_else(|| format!("{label} must be an unsigned integer"))?;
    u32::try_from(raw).map_err(|_| format!("{label} exceeds u32"))
}

fn usize_to_u32(value: usize, label: &str) -> Result<u32, String> {
    u32::try_from(value).map_err(|_| format!("{label} exceeds u32"))
}

fn write_u32(out: &mut Vec<u8>, value: u32) {
    out.extend_from_slice(&value.to_le_bytes());
}

fn parse_asp_cst(text: &str, settings: &Value) -> Value {
    let index = TextIndex::new(text);
    let scan = scan_html_and_asp(&index, settings);
    let directives = scan
        .inline_regions
        .iter()
        .filter(|region| region.kind == "asp-directive")
        .map(|region| directive_from_region(&index, region))
        .collect::<Vec<_>>();
    let default_language = default_language_from_directives(&directives, settings);
    let mut embedded = scan.inline_regions.clone();
    embedded.extend(scan.tag_regions.into_iter().map(|mut region| {
        if region.kind == "server-script" {
            let language = region
                .attributes
                .get("language")
                .and_then(Value::as_str)
                .unwrap_or(default_language.as_str());
            region.language = if normalize_script_language(language) == "JScript" {
                "jscript".to_string()
            } else {
                "vbscript".to_string()
            };
        }
        region
    }));
    let regions = build_regions(&index, embedded, &default_language);
    let mut nodes = regions
        .iter()
        .map(|region| region_to_node(&index, region))
        .collect::<Vec<_>>();
    nodes.extend(
        scan.includes
            .iter()
            .map(|include| include_to_node(&index, include)),
    );
    nodes.sort_by(|left, right| {
        let ls = value_usize(left, "start");
        let rs = value_usize(right, "start");
        let le = value_usize(left, "end");
        let re = value_usize(right, "end");
        (ls, le.saturating_sub(ls)).cmp(&(rs, re.saturating_sub(rs)))
    });
    for directive in directives {
        let offset = directive.get("offset").and_then(Value::as_u64).unwrap_or(0);
        for node in &mut nodes {
            if value_usize(node, "start") as u64 == offset
                && node.get("kind").and_then(Value::as_str) == Some("AspDirective")
            {
                if let Some(object) = node.as_object_mut() {
                    object.insert("directive".to_string(), directive.clone());
                    object.insert(
                        "attributes".to_string(),
                        directive
                            .get("attributes")
                            .cloned()
                            .unwrap_or_else(|| json!({})),
                    );
                }
            }
        }
    }
    let errors = scan
        .diagnostics
        .iter()
        .map(|diagnostic| {
            json!({
                "message": diagnostic.message,
                "start": diagnostic.start,
                "end": diagnostic.end,
            })
        })
        .collect::<Vec<_>>();
    let tokens = nodes
        .iter()
        .flat_map(|node| {
            node.get("tokens")
                .and_then(Value::as_array)
                .cloned()
                .unwrap_or_default()
        })
        .collect::<Vec<_>>();
    json!({
        "kind": "Document",
        "start": 0,
        "end": index.len(),
        "contentStart": 0,
        "contentEnd": index.len(),
        "text": text,
        "tokens": tokens,
        "children": nodes,
        "serverObjects": scan.server_objects,
        "errors": errors,
    })
}

#[derive(Clone)]
struct DiagnosticSpan {
    message: String,
    start: usize,
    end: usize,
}

#[derive(Clone)]
struct AspScan {
    inline_regions: Vec<Region>,
    tag_regions: Vec<Region>,
    includes: Vec<IncludeRef>,
    server_objects: Vec<Value>,
    diagnostics: Vec<DiagnosticSpan>,
}

#[derive(Clone)]
struct Region {
    kind: String,
    language: String,
    start: usize,
    end: usize,
    content_start: usize,
    content_end: usize,
    attributes: JsonMap,
}

#[derive(Clone)]
struct IncludeRef {
    range: Value,
    offset: usize,
    path: String,
    mode: String,
    directive_range: Value,
    mode_range: Value,
    path_range: Value,
}

#[derive(Clone)]
struct HtmlTag {
    name: String,
    start: usize,
    end: usize,
    attributes_start: usize,
    attributes_end: usize,
    attributes: JsonMap,
    attribute_spans: Vec<AttributeSpan>,
    closing: bool,
    self_closing: bool,
}

#[derive(Clone)]
struct AttributeSpan {
    name: String,
    value: Value,
    value_start: usize,
    value_end: usize,
}

fn scan_html_and_asp(index: &TextIndex<'_>, settings: &Value) -> AspScan {
    let mut scan = AspScan {
        inline_regions: Vec::new(),
        tag_regions: Vec::new(),
        includes: Vec::new(),
        server_objects: Vec::new(),
        diagnostics: Vec::new(),
    };
    let mut script_language = normalize_script_language(
        settings
            .get("defaultLanguage")
            .and_then(Value::as_str)
            .unwrap_or("VBScript"),
    );
    let mut cursor = 0usize;
    while cursor < index.len() {
        if index.starts_with(cursor, "<%") {
            let region = parse_asp_region_at(
                index,
                cursor,
                index.len(),
                settings,
                &mut scan,
                &script_language,
            );
            cursor = region.end;
            if let Some(language) = script_language_from_directive_region(index, &region) {
                script_language = language;
            }
            scan.inline_regions.push(region);
            continue;
        }
        if index.starts_with(cursor, "<!--") {
            let end = find_string(index, "-->", cursor + 4)
                .map(|offset| offset + 3)
                .unwrap_or_else(|| index.len());
            if let Some(include) = parse_include_comment(index, cursor, end) {
                scan.includes.push(include);
            }
            cursor = end;
            continue;
        }
        if index.char_at(cursor) != Some('<') {
            cursor += 1;
            continue;
        }
        let Some(tag) = read_html_tag(index, cursor, &script_language) else {
            cursor += 1;
            continue;
        };
        if !tag.closing {
            scan.tag_regions
                .extend(style_attribute_regions_from_tag(&tag));
            if let Some(server_object) = server_object_from_tag(index, &tag) {
                scan.server_objects.push(server_object);
            }
            let nested = scan_asp_regions_in_range(
                index,
                tag.attributes_start,
                tag.attributes_end,
                settings,
                &mut scan,
                &script_language,
            );
            scan.inline_regions.extend(nested);
        }
        if (tag.name == "script" || tag.name == "style") && !tag.closing && !tag.self_closing {
            if let Some((close_start, close_end)) = find_element_close(index, &tag.name, tag.end) {
                scan.tag_regions
                    .push(element_region_from_tag(&tag, close_start, close_end));
                let nested = scan_asp_regions_in_range(
                    index,
                    tag.end,
                    close_start,
                    settings,
                    &mut scan,
                    &script_language,
                );
                scan.inline_regions.extend(nested);
                cursor = close_end;
                continue;
            }
        }
        cursor = tag.end;
    }
    scan
}

fn scan_asp_regions_in_range(
    index: &TextIndex<'_>,
    start: usize,
    end: usize,
    settings: &Value,
    scan: &mut AspScan,
    script_language: &str,
) -> Vec<Region> {
    let mut regions = Vec::new();
    let mut cursor = start;
    while cursor < end {
        let Some(next) = find_string(index, "<%", cursor) else {
            break;
        };
        if next >= end {
            break;
        }
        let region = parse_asp_region_at(index, next, end, settings, scan, script_language);
        cursor = region.end.max(next + 2);
        regions.push(region);
    }
    regions
}

fn parse_asp_region_at(
    index: &TextIndex<'_>,
    start: usize,
    max_end: usize,
    settings: &Value,
    scan: &mut AspScan,
    script_language: &str,
) -> Region {
    let close = find_asp_close(index, start + 2, max_end, script_language);
    if close.is_none() {
        scan.diagnostics.push(DiagnosticSpan {
            message: missing_asp_close_message(settings),
            start,
            end: max_end,
        });
    }
    let marker = index.char_at(start + 2);
    let kind = if marker == Some('=') {
        "asp-expression"
    } else if marker == Some('@') {
        "asp-directive"
    } else {
        "asp-block"
    };
    let content_start = start
        + if marker == Some('=') || marker == Some('@') {
            3
        } else {
            2
        };
    let content_end = close.unwrap_or(max_end);
    Region {
        kind: kind.to_string(),
        language: if kind == "asp-directive" {
            "asp-directive".to_string()
        } else {
            "vbscript".to_string()
        },
        start,
        end: close.map(|offset| offset + 2).unwrap_or(max_end),
        content_start,
        content_end,
        attributes: JsonMap::new(),
    }
}

fn script_language_from_directive_region(index: &TextIndex<'_>, region: &Region) -> Option<String> {
    if region.kind != "asp-directive" {
        return None;
    }
    let raw = index.slice(region.content_start, region.content_end).trim();
    let normalized = raw.strip_prefix('@').unwrap_or(raw).trim();
    let mut parts = normalized.split_whitespace();
    let first = parts.next().unwrap_or("Page");
    let attribute_text = if first.contains('=') {
        normalized.to_string()
    } else {
        parts.collect::<Vec<_>>().join(" ")
    };
    parse_attributes(&attribute_text)
        .get("language")
        .and_then(Value::as_str)
        .map(normalize_script_language)
}

fn missing_asp_close_message(settings: &Value) -> String {
    let locale = settings
        .get("resolvedLocale")
        .and_then(Value::as_str)
        .unwrap_or("en");
    if locale == "ja" {
        "Classic ASP ブロックに閉じ区切り %> がありません。".to_string()
    } else {
        "Classic ASP block is missing a closing %> delimiter.".to_string()
    }
}

fn find_asp_close(
    index: &TextIndex<'_>,
    offset: usize,
    max_end: usize,
    _script_language: &str,
) -> Option<usize> {
    let mut cursor = offset;
    while cursor < max_end {
        let char = index.char_at(cursor).unwrap_or('\0');
        let next = index.char_at(cursor + 1).unwrap_or('\0');
        if char == '%' && next == '>' {
            return Some(cursor);
        }
        cursor += 1;
    }
    None
}

fn parse_include_comment(index: &TextIndex<'_>, start: usize, end: usize) -> Option<IncludeRef> {
    let comment = index.slice(start, end);
    let include_at = comment.to_ascii_lowercase().find("#include")?;
    let directive_start = start + include_at;
    let after_directive = directive_start + "#include".len();
    let mut cursor = skip_ascii_ws(index, after_directive, end);
    let mode_start = cursor;
    while cursor < end {
        let ch = index.char_at(cursor).unwrap_or('\0');
        if ch == '=' || ch.is_ascii_whitespace() {
            break;
        }
        cursor += 1;
    }
    let mode = index.slice(mode_start, cursor).to_ascii_lowercase();
    cursor = skip_ascii_ws(index, cursor, end);
    if index.char_at(cursor) != Some('=') {
        return None;
    }
    cursor = skip_ascii_ws(index, cursor + 1, end);
    let quote = index.char_at(cursor)?;
    if quote != '"' && quote != '\'' {
        return None;
    }
    let path_start = cursor;
    cursor += 1;
    while cursor < end && index.char_at(cursor) != Some(quote) {
        cursor += 1;
    }
    if cursor >= end {
        return None;
    }
    let path_end = cursor + 1;
    let mode_len = mode.len();
    Some(IncludeRef {
        range: index.range(start, end),
        offset: start,
        path: index.slice(path_start + 1, cursor).to_string(),
        mode,
        directive_range: index.range(directive_start, directive_start + "#include".len()),
        mode_range: index.range(mode_start, mode_start + mode_len),
        path_range: index.range(path_start, path_end),
    })
}

fn read_html_tag(index: &TextIndex<'_>, start: usize, script_language: &str) -> Option<HtmlTag> {
    if index.char_at(start) != Some('<')
        || index.starts_with(start, "<!--")
        || index.char_at(start + 1) == Some('%')
    {
        return None;
    }
    let mut cursor = start + 1;
    let closing = index.char_at(cursor) == Some('/');
    if closing {
        cursor += 1;
    }
    cursor = skip_html_ws(index, cursor, index.len());
    let name_start = cursor;
    if !is_ascii_alpha(index.char_at(cursor)) {
        return None;
    }
    cursor += 1;
    while is_html_tag_part(index.char_at(cursor)) {
        cursor += 1;
    }
    let name = index.slice(name_start, cursor).to_ascii_lowercase();
    let tag_end = find_tag_end(index, cursor, script_language)?;
    let attributes_start = cursor;
    let attributes_end = tag_end;
    let attribute_spans =
        parse_attribute_spans(index, attributes_start, attributes_end, script_language);
    let mut attributes = JsonMap::new();
    for attribute in &attribute_spans {
        attributes.insert(attribute.name.clone(), attribute.value.clone());
        attributes.insert(attribute.name.to_ascii_lowercase(), attribute.value.clone());
    }
    let self_closing = index
        .slice(attributes_start, attributes_end)
        .trim_end()
        .ends_with('/');
    Some(HtmlTag {
        name,
        start,
        end: tag_end + 1,
        attributes_start,
        attributes_end,
        attributes,
        attribute_spans,
        closing,
        self_closing,
    })
}

fn find_tag_end(index: &TextIndex<'_>, offset: usize, script_language: &str) -> Option<usize> {
    let mut quote: Option<char> = None;
    let mut cursor = offset;
    while cursor < index.len() {
        let ch = index.char_at(cursor).unwrap_or('\0');
        if let Some(current_quote) = quote {
            if ch == current_quote {
                quote = None;
            }
        } else if ch == '"' || ch == '\'' {
            quote = Some(ch);
        } else if index.starts_with(cursor, "<%") {
            let close = find_asp_close(index, cursor + 2, index.len(), script_language)?;
            cursor = close + 1;
        } else if ch == '>' {
            return Some(cursor);
        }
        cursor += 1;
    }
    None
}

fn parse_attribute_spans(
    index: &TextIndex<'_>,
    start: usize,
    end: usize,
    script_language: &str,
) -> Vec<AttributeSpan> {
    let mut attributes = Vec::new();
    let mut cursor = start;
    while cursor < end {
        while cursor < end {
            let ch = index.char_at(cursor).unwrap_or('\0');
            if ch != '/' && !is_html_ws(ch) {
                break;
            }
            cursor += 1;
        }
        if index.starts_with(cursor, "<%") {
            cursor = find_asp_close(index, cursor + 2, end, script_language)
                .map(|offset| offset + 2)
                .unwrap_or(end);
            continue;
        }
        let name_start = cursor;
        if !is_attr_name_start(index.char_at(cursor)) {
            cursor += 1;
            continue;
        }
        cursor += 1;
        while is_attr_name_part(index.char_at(cursor)) {
            cursor += 1;
        }
        let name = index.slice(name_start, cursor).to_string();
        cursor = skip_html_ws(index, cursor, end);
        if index.char_at(cursor) != Some('=') {
            attributes.push(AttributeSpan {
                name,
                value: Value::Bool(true),
                value_start: cursor,
                value_end: cursor,
            });
            continue;
        }
        cursor = skip_html_ws(index, cursor + 1, end);
        let quote = index.char_at(cursor).filter(|ch| *ch == '"' || *ch == '\'');
        let value_start = if quote.is_some() { cursor + 1 } else { cursor };
        if let Some(quote) = quote {
            cursor += 1;
            while cursor < end && index.char_at(cursor) != Some(quote) {
                cursor += 1;
            }
            let value_end = cursor;
            if cursor < end {
                cursor += 1;
            }
            attributes.push(AttributeSpan {
                name,
                value: Value::String(index.slice(value_start, value_end).to_string()),
                value_start,
                value_end,
            });
            continue;
        }
        while cursor < end {
            let ch = index.char_at(cursor).unwrap_or('\0');
            if ch == '>' || is_html_ws(ch) {
                break;
            }
            cursor += 1;
        }
        attributes.push(AttributeSpan {
            name,
            value: Value::String(index.slice(value_start, cursor).to_string()),
            value_start,
            value_end: cursor,
        });
    }
    attributes
}

fn style_attribute_regions_from_tag(tag: &HtmlTag) -> Vec<Region> {
    tag.attribute_spans
        .iter()
        .filter(|attribute| {
            attribute.name.eq_ignore_ascii_case("style") && attribute.value.is_string()
        })
        .map(|attribute| {
            let mut attributes = JsonMap::new();
            attributes.insert("tagName".to_string(), Value::String(tag.name.clone()));
            Region {
                kind: "style-attribute".to_string(),
                language: "css".to_string(),
                start: attribute.value_start,
                end: attribute.value_end,
                content_start: attribute.value_start,
                content_end: attribute.value_end,
                attributes,
            }
        })
        .collect()
}

fn server_object_from_tag(index: &TextIndex<'_>, tag: &HtmlTag) -> Option<Value> {
    if tag.name != "object" {
        return None;
    }
    let runat_server = tag
        .attributes
        .get("runat")
        .and_then(Value::as_str)
        .map(|value| value.eq_ignore_ascii_case("server"))
        .unwrap_or(false);
    if !runat_server {
        return None;
    }
    let id_span = attribute_span_by_name(tag, "id")?;
    let id = id_span.value.as_str()?.to_string();
    if id.is_empty() {
        return None;
    }
    let prog_id_span = attribute_span_by_name(tag, "progid");
    let class_id_span = attribute_span_by_name(tag, "classid");
    let prog_id = prog_id_span
        .and_then(|attribute| attribute.value.as_str())
        .map(str::to_string);
    let class_id = class_id_span
        .and_then(|attribute| attribute.value.as_str())
        .map(str::to_string);
    let mut object = JsonMap::new();
    object.insert("range".to_string(), index.range(tag.start, tag.end));
    object.insert("offset".to_string(), json!(tag.start));
    object.insert("id".to_string(), Value::String(id));
    object.insert(
        "idRange".to_string(),
        index.range(id_span.value_start, id_span.value_end),
    );
    if let Some(value) = prog_id {
        object.insert("progId".to_string(), Value::String(value));
    }
    if let Some(attribute) = prog_id_span {
        if attribute.value.is_string() {
            object.insert(
                "progIdRange".to_string(),
                index.range(attribute.value_start, attribute.value_end),
            );
        }
    }
    if let Some(value) = class_id {
        object.insert("classId".to_string(), Value::String(value));
    }
    if let Some(attribute) = class_id_span {
        if attribute.value.is_string() {
            object.insert(
                "classIdRange".to_string(),
                index.range(attribute.value_start, attribute.value_end),
            );
        }
    }
    object.insert(
        "attributes".to_string(),
        Value::Object(tag.attributes.clone()),
    );
    Some(Value::Object(object))
}

fn attribute_span_by_name<'a>(tag: &'a HtmlTag, name: &str) -> Option<&'a AttributeSpan> {
    tag.attribute_spans
        .iter()
        .find(|attribute| attribute.name.eq_ignore_ascii_case(name))
}

fn element_region_from_tag(tag: &HtmlTag, close_start: usize, close_end: usize) -> Region {
    if tag.name == "style" {
        return Region {
            kind: "style".to_string(),
            language: "css".to_string(),
            start: tag.start,
            end: close_end,
            content_start: tag.end,
            content_end: close_start,
            attributes: tag.attributes.clone(),
        };
    }
    let runat_server = tag
        .attributes
        .get("runat")
        .and_then(Value::as_str)
        .map(|value| value.eq_ignore_ascii_case("server"))
        .unwrap_or(false);
    let language = tag
        .attributes
        .get("language")
        .or_else(|| tag.attributes.get("type"))
        .and_then(Value::as_str)
        .unwrap_or_default();
    Region {
        kind: if runat_server {
            "server-script"
        } else {
            "client-script"
        }
        .to_string(),
        language: if runat_server {
            if normalize_script_language(language) == "JScript" {
                "jscript"
            } else {
                "vbscript"
            }
        } else {
            "javascript"
        }
        .to_string(),
        start: tag.start,
        end: close_end,
        content_start: tag.end,
        content_end: close_start,
        attributes: tag.attributes.clone(),
    }
}

fn find_element_close(
    index: &TextIndex<'_>,
    tag_name: &str,
    offset: usize,
) -> Option<(usize, usize)> {
    let close = format!("</{tag_name}");
    let mut cursor = offset;
    while cursor < index.len() {
        if index
            .slice(cursor, index.len())
            .to_ascii_lowercase()
            .starts_with(&close)
        {
            let end = find_tag_end(index, cursor + 2, "VBScript")?;
            return Some((cursor, end + 1));
        }
        cursor += 1;
    }
    None
}

fn directive_from_region(index: &TextIndex<'_>, region: &Region) -> Value {
    let raw = index.slice(region.content_start, region.content_end).trim();
    let normalized = raw.strip_prefix('@').unwrap_or(raw).trim();
    let mut parts = normalized.split_whitespace();
    let first = parts.next().unwrap_or("Page");
    let has_explicit_name = !first.contains('=');
    let name = if has_explicit_name { first } else { "Page" };
    let attribute_text = if has_explicit_name {
        parts.collect::<Vec<_>>().join(" ")
    } else {
        normalized.to_string()
    };
    json!({
        "offset": region.start,
        "range": index.range(region.start, region.end),
        "name": name,
        "attributes": parse_attributes(&attribute_text),
    })
}

fn parse_attributes(text: &str) -> Value {
    let index = TextIndex::new(text);
    let spans = parse_attribute_spans(&index, 0, index.len(), "VBScript");
    let mut object = JsonMap::new();
    for span in spans {
        object.insert(span.name.clone(), span.value.clone());
        object.insert(span.name.to_ascii_lowercase(), span.value);
    }
    Value::Object(object)
}

fn default_language_from_directives(directives: &[Value], settings: &Value) -> String {
    let directive_language = directives.iter().find_map(|directive| {
        directive
            .get("attributes")
            .and_then(|attributes| {
                attributes
                    .get("language")
                    .or_else(|| attributes.get("LANGUAGE"))
                    .and_then(Value::as_str)
            })
            .map(str::to_string)
    });
    let configured = settings
        .get("defaultLanguage")
        .and_then(Value::as_str)
        .unwrap_or("VBScript");
    normalize_script_language(directive_language.as_deref().unwrap_or(configured))
}

fn normalize_script_language(language: &str) -> String {
    let lower = language.to_ascii_lowercase();
    if lower.contains("jscript") || lower.contains("javascript") {
        "JScript".to_string()
    } else {
        "VBScript".to_string()
    }
}

fn build_regions(
    index: &TextIndex<'_>,
    mut embedded: Vec<Region>,
    default_language: &str,
) -> Vec<Region> {
    embedded.retain(|region| region.end > region.start);
    embedded.sort_by(|left, right| {
        (left.start, usize::MAX - left.end).cmp(&(right.start, usize::MAX - right.end))
    });
    let mut accepted = Vec::new();
    let mut top_level = Vec::new();
    let mut covered_end = 0usize;
    let mut has_covered = false;
    for mut region in embedded {
        if region.language == "vbscript"
            && default_language == "JScript"
            && (region.kind == "asp-block" || region.kind == "asp-expression")
        {
            region.language = "jscript".to_string();
        }
        if has_covered && region.start < covered_end {
            if region.kind == "asp-block"
                || region.kind == "asp-expression"
                || region.kind == "asp-directive"
            {
                accepted.push(region);
            }
            continue;
        }
        covered_end = region.end;
        has_covered = true;
        top_level.push(region.clone());
        accepted.push(region);
    }
    let mut regions = Vec::new();
    let mut cursor = 0usize;
    for region in &top_level {
        if cursor < region.start {
            regions.push(html_region(cursor, region.start));
        }
        if has_html_wrapper(region) && region.start < region.content_start {
            regions.push(html_region(region.start, region.content_start));
        }
        regions.push(region.clone());
        if has_html_wrapper(region) && region.content_end < region.end {
            regions.push(html_region(region.content_end, region.end));
        }
        cursor = region.end;
    }
    if cursor < index.len() {
        regions.push(html_region(cursor, index.len()));
    }
    let top_keys = top_level
        .iter()
        .map(|region| (region.start, region.end, region.kind.clone()))
        .collect::<HashSet<_>>();
    regions.extend(
        accepted
            .into_iter()
            .filter(|region| !top_keys.contains(&(region.start, region.end, region.kind.clone()))),
    );
    regions.sort_by(|left, right| {
        (left.start, left.end.saturating_sub(left.start))
            .cmp(&(right.start, right.end.saturating_sub(right.start)))
    });
    regions
}

fn has_html_wrapper(region: &Region) -> bool {
    region.kind == "style" || region.kind == "client-script" || region.kind == "server-script"
}

fn html_region(start: usize, end: usize) -> Region {
    Region {
        kind: "html".to_string(),
        language: "html".to_string(),
        start,
        end,
        content_start: start,
        content_end: end,
        attributes: JsonMap::new(),
    }
}

fn region_to_node(index: &TextIndex<'_>, region: &Region) -> Value {
    let kind = match region.kind.as_str() {
        "html" => "HtmlText",
        "asp-expression" => "AspExpression",
        "asp-directive" => "AspDirective",
        "style" => "StyleElement",
        "client-script" => "ClientScriptElement",
        "server-script" => "ServerScriptElement",
        "style-attribute" => "StyleAttribute",
        _ => "AspBlock",
    };
    let mut object = JsonMap::new();
    object.insert("kind".to_string(), Value::String(kind.to_string()));
    object.insert("start".to_string(), json!(region.start));
    object.insert("end".to_string(), json!(region.end));
    object.insert("contentStart".to_string(), json!(region.content_start));
    object.insert("contentEnd".to_string(), json!(region.content_end));
    object.insert(
        "language".to_string(),
        Value::String(region.language.clone()),
    );
    object.insert(
        "text".to_string(),
        Value::String(index.slice(region.start, region.end).to_string()),
    );
    object.insert(
        "tokens".to_string(),
        Value::Array(region_tokens(index, region)),
    );
    object.insert("children".to_string(), Value::Array(Vec::new()));
    if !region.attributes.is_empty() {
        object.insert(
            "attributes".to_string(),
            Value::Object(region.attributes.clone()),
        );
    }
    object.insert("regionKind".to_string(), Value::String(region.kind.clone()));
    if region.language == "vbscript" {
        object.insert(
            "vbscript".to_string(),
            parse_vbscript_cst(
                index.slice(region.content_start, region.content_end),
                index.text,
                region.content_start,
            ),
        );
    }
    Value::Object(object)
}

fn include_to_node(index: &TextIndex<'_>, include: &IncludeRef) -> Value {
    let end = include
        .range
        .get("end")
        .and_then(|position| offset_from_position(index, position))
        .unwrap_or(include.offset);
    json!({
        "kind": "IncludeDirective",
        "start": include.offset,
        "end": end,
        "contentStart": include.offset,
        "contentEnd": end,
        "language": "html",
        "text": index.slice(include.offset, end),
        "tokens": [{
            "kind": "includeDirective",
            "start": include.offset,
            "end": end,
            "text": index.slice(include.offset, end),
        }],
        "children": [],
        "include": include_to_value(include),
    })
}

fn include_to_value(include: &IncludeRef) -> Value {
    json!({
        "range": include.range,
        "offset": include.offset,
        "path": include.path,
        "mode": include.mode,
        "directiveRange": include.directive_range,
        "modeRange": include.mode_range,
        "pathRange": include.path_range,
    })
}

fn region_tokens(index: &TextIndex<'_>, region: &Region) -> Vec<Value> {
    if region.kind == "asp-block"
        || region.kind == "asp-expression"
        || region.kind == "asp-directive"
    {
        let open_kind = if region.kind == "asp-expression" {
            "aspExpressionOpen"
        } else if region.kind == "asp-directive" {
            "aspDirectiveOpen"
        } else {
            "aspOpen"
        };
        return vec![
            token_json(open_kind, index, region.start, region.content_start),
            token_json("text", index, region.content_start, region.content_end),
            token_json("aspClose", index, region.content_end, region.end),
        ];
    }
    if region.kind == "html" {
        return vec![token_json("text", index, region.start, region.end)];
    }
    vec![
        token_json("tagOpen", index, region.start, region.content_start),
        token_json("text", index, region.content_start, region.content_end),
        token_json("tagClose", index, region.content_end, region.end),
    ]
}

fn token_json(kind: &str, index: &TextIndex<'_>, start: usize, end: usize) -> Value {
    json!({
        "kind": kind,
        "start": start,
        "end": end,
        "text": index.slice(start, end),
    })
}

#[derive(Clone)]
struct VbToken {
    kind: String,
    start: usize,
    end: usize,
    text: String,
    value: Option<String>,
}

#[derive(Clone)]
struct VbNode {
    kind: String,
    start: usize,
    end: usize,
    content_start: Option<usize>,
    content_end: Option<usize>,
    name_token: Option<VbToken>,
    tokens: Vec<VbToken>,
    children: Vec<VbNode>,
    procedure_kind: Option<String>,
    property_accessor: Option<String>,
    declaration_kind: Option<String>,
    visibility: Option<String>,
    identifiers: Vec<VbToken>,
    array_declarations: Vec<Value>,
    parameters: Vec<VbToken>,
    parameter_metadata: Vec<Value>,
    type_name: Option<String>,
    member_of: Option<String>,
    scope_name: Option<String>,
    scope_start: Option<usize>,
    scope_end: Option<usize>,
}

fn parse_vbscript_cst(text: &str, source_text: &str, base_offset: usize) -> Value {
    let index = TextIndex::new(text);
    let text_len = index.len();
    let tokens = tokenize_vbscript(&index, base_offset);
    let significant = tokens
        .iter()
        .filter(|token| token.kind != "whitespace" && token.kind != "comment")
        .cloned()
        .collect::<Vec<_>>();
    let mut document = VbNode {
        kind: "Document".to_string(),
        start: base_offset,
        end: base_offset + text_len,
        content_start: Some(base_offset),
        content_end: Some(base_offset + text_len),
        name_token: None,
        tokens: tokens.clone(),
        children: Vec::new(),
        procedure_kind: None,
        property_accessor: None,
        declaration_kind: None,
        visibility: None,
        identifiers: Vec::new(),
        array_declarations: Vec::new(),
        parameters: Vec::new(),
        parameter_metadata: Vec::new(),
        type_name: None,
        member_of: None,
        scope_name: None,
        scope_start: None,
        scope_end: None,
    };
    let mut stack: Vec<VbNode> = Vec::new();
    for i in 0..significant.len() {
        let token = &significant[i];
        if !is_statement_start(&significant, i) {
            continue;
        }
        let first = lower_token(Some(token));
        let second = lower_token(significant.get(i + 1));
        if first == "class" && significant.get(i + 1).map(|t| t.kind.as_str()) == Some("identifier")
        {
            let node = create_block_node("Class", token, &significant[i + 1], &stack);
            push_child(&mut document, &mut stack, node.clone());
            stack.push(node);
            continue;
        }
        if first == "end" {
            close_block(&mut document, &mut stack, &second, token.end);
            continue;
        }
        let declaration_start = if first == "public" || first == "private" {
            second.clone()
        } else {
            first.clone()
        };
        let declaration_offset = if first == "public" || first == "private" {
            1
        } else {
            0
        };
        let visibility = if first == "public" || first == "private" {
            Some(first.clone())
        } else {
            None
        };
        if declaration_start == "sub" || declaration_start == "function" {
            if let Some(name) = significant.get(i + declaration_offset + 1) {
                if name.kind == "identifier" {
                    let node = create_procedure_node(
                        &declaration_start,
                        token,
                        name,
                        collect_parameter_metadata(&significant, i + declaration_offset + 2),
                        &stack,
                        None,
                        visibility.clone(),
                    );
                    push_child(&mut document, &mut stack, node.clone());
                    stack.push(node);
                }
            }
            continue;
        }
        if declaration_start == "property" {
            let accessor = lower_token(significant.get(i + declaration_offset + 1));
            if accessor == "get" || accessor == "let" || accessor == "set" {
                if let Some(name) = significant.get(i + declaration_offset + 2) {
                    if name.kind == "identifier" {
                        let node = create_procedure_node(
                            "property",
                            token,
                            name,
                            collect_parameter_metadata(&significant, i + declaration_offset + 3),
                            &stack,
                            Some(accessor),
                            visibility.clone(),
                        );
                        push_child(&mut document, &mut stack, node.clone());
                        stack.push(node);
                    }
                }
            }
            continue;
        }
        if first == "loop" {
            close_block(&mut document, &mut stack, &"loop".to_string(), token.end);
            continue;
        }
        if first == "wend" {
            close_block(&mut document, &mut stack, &"wend".to_string(), token.end);
            continue;
        }
        if first == "next" {
            close_block(&mut document, &mut stack, &"next".to_string(), token.end);
            continue;
        }
        if first == "if" {
            let node = create_statement_node("If", token, &significant, i, None);
            let multiline = is_multiline_if(&significant, i);
            push_child(&mut document, &mut stack, node.clone());
            if multiline {
                stack.push(node);
            }
            continue;
        }
        if first == "select" && second == "case" {
            let node = create_statement_node("Select", token, &significant, i, None);
            push_child(&mut document, &mut stack, node.clone());
            stack.push(node);
            continue;
        }
        if first == "do" || first == "while" {
            let kind = if first == "do" { "DoLoop" } else { "While" };
            let node = create_statement_node(kind, token, &significant, i, None);
            push_child(&mut document, &mut stack, node.clone());
            stack.push(node);
            continue;
        }
        if first == "dim" || first == "redim" {
            let node = create_declaration_node(
                token,
                "VariableDeclaration",
                &first,
                &significant,
                i + 1,
                None,
            );
            push_child(&mut document, &mut stack, node);
            continue;
        }
        if (first == "public" || first == "private")
            && second != "sub"
            && second != "function"
            && second != "property"
        {
            let node = create_declaration_node(
                token,
                "VariableDeclaration",
                &first,
                &significant,
                i + 1,
                visibility.clone(),
            );
            push_child(&mut document, &mut stack, node);
            continue;
        }
        if first == "const" {
            let node = create_declaration_node(
                token,
                "ConstantDeclaration",
                "const",
                &significant,
                i + 1,
                None,
            );
            push_child(&mut document, &mut stack, node);
            continue;
        }
        if first == "for" && second == "each" {
            if let Some(name) = significant
                .get(i + 2)
                .filter(|token| token.kind == "identifier")
            {
                let mut node =
                    create_statement_node("ForEach", token, &significant, i, Some(name.clone()));
                node.declaration_kind = Some("forEach".to_string());
                node.identifiers.push(name.clone());
                push_child(&mut document, &mut stack, node.clone());
                stack.push(node);
            }
            continue;
        }
        if first == "with" {
            if let Some(name) = significant
                .get(i + 1)
                .filter(|token| token.kind == "identifier")
            {
                let mut node =
                    create_statement_node("With", token, &significant, i, Some(name.clone()));
                node.scope_end = Some(base_offset + TextIndex::new(source_text).len());
                push_child(&mut document, &mut stack, node.clone());
                stack.push(node);
            }
            continue;
        }
        if first == "set"
            && significant.get(i + 1).map(|t| t.kind.as_str()) == Some("identifier")
            && significant.get(i + 2).map(|t| t.text.as_str()) == Some("=")
        {
            let variable = significant[i + 1].clone();
            let end_index = statement_end_index(&significant, i);
            if let Some(new_index) = find_keyword(&significant, i + 3, end_index, "new") {
                if let Some(name) = significant
                    .get(new_index + 1)
                    .filter(|token| token.kind == "identifier")
                {
                    let mut node =
                        create_statement_node("SetNew", token, &significant, i, Some(variable));
                    node.type_name = Some(name.text.clone());
                    push_child(&mut document, &mut stack, node);
                }
            } else if let Some(create_index) =
                find_create_object_call(&significant, i + 3, end_index)
            {
                if let Some(string_token) = significant[create_index..=end_index]
                    .iter()
                    .find(|token| token.kind == "string")
                {
                    let mut node = create_statement_node(
                        "CreateObject",
                        token,
                        &significant,
                        i,
                        Some(variable),
                    );
                    node.type_name = Some(
                        string_token
                            .value
                            .clone()
                            .unwrap_or_else(|| unquote_vb_string(&string_token.text)),
                    );
                    push_child(&mut document, &mut stack, node);
                }
            }
            continue;
        }
        if first == "call" {
            let end_index = statement_end_index(&significant, i);
            let name = significant[i + 1..=end_index]
                .iter()
                .find(|token| token.kind == "identifier")
                .cloned();
            let node = create_statement_node("Call", token, &significant, i, name);
            push_child(&mut document, &mut stack, node);
            continue;
        }
        if token.kind == "identifier" && statement_has_symbol(&significant, i, "=") {
            let node =
                create_statement_node("Assignment", token, &significant, i, Some(token.clone()));
            push_child(&mut document, &mut stack, node);
            continue;
        }
        let node = create_statement_node("Expression", token, &significant, i, None);
        push_child(&mut document, &mut stack, node);
    }
    close_unclosed(&mut document, &mut stack, base_offset + text_len);
    vb_node_to_value(&document)
}

fn tokenize_vbscript(index: &TextIndex<'_>, base_offset: usize) -> Vec<VbToken> {
    let mut tokens = Vec::new();
    let mut cursor = 0usize;
    while cursor < index.len() {
        let start = cursor;
        let ch = index.char_at(cursor).unwrap_or('\0');
        if ch == '\r' || ch == '\n' {
            if ch == '\r' && index.char_at(cursor + 1) == Some('\n') {
                cursor += 2;
            } else {
                cursor += 1;
            }
            tokens.push(vb_token("newline", index, start, cursor, base_offset, None));
            continue;
        }
        if ch == ' ' || ch == '\t' {
            while cursor < index.len() {
                let c = index.char_at(cursor).unwrap_or('\0');
                if c != ' ' && c != '\t' {
                    break;
                }
                cursor += 1;
            }
            tokens.push(vb_token(
                "whitespace",
                index,
                start,
                cursor,
                base_offset,
                None,
            ));
            continue;
        }
        if ch == '\'' {
            while cursor < index.len() {
                let c = index.char_at(cursor).unwrap_or('\0');
                if c == '\r' || c == '\n' {
                    break;
                }
                cursor += 1;
            }
            tokens.push(vb_token("comment", index, start, cursor, base_offset, None));
            continue;
        }
        if is_rem_comment_start(index, cursor) {
            cursor += 3;
            while cursor < index.len() {
                let c = index.char_at(cursor).unwrap_or('\0');
                if c == '\r' || c == '\n' {
                    break;
                }
                cursor += 1;
            }
            tokens.push(vb_token("comment", index, start, cursor, base_offset, None));
            continue;
        }
        if ch == '"' {
            cursor += 1;
            while cursor < index.len() {
                if index.char_at(cursor) == Some('"') && index.char_at(cursor + 1) == Some('"') {
                    cursor += 2;
                    continue;
                }
                if index.char_at(cursor) == Some('"') {
                    cursor += 1;
                    break;
                }
                cursor += 1;
            }
            let text_value = index.slice(start, cursor).to_string();
            tokens.push(vb_token(
                "string",
                index,
                start,
                cursor,
                base_offset,
                Some(unquote_vb_string(&text_value)),
            ));
            continue;
        }
        if is_identifier_start(ch) {
            cursor += 1;
            while is_identifier_part(index.char_at(cursor).unwrap_or('\0')) {
                cursor += 1;
            }
            let token_text = index.slice(start, cursor).to_string();
            let kind = if vb_keywords().contains(token_text.to_ascii_lowercase().as_str()) {
                "keyword"
            } else {
                "identifier"
            };
            tokens.push(vb_token(kind, index, start, cursor, base_offset, None));
            continue;
        }
        if ch.is_ascii_digit() {
            cursor += 1;
            while {
                let c = index.char_at(cursor).unwrap_or('\0');
                c == '.' || c.is_ascii_digit()
            } {
                cursor += 1;
            }
            tokens.push(vb_token("number", index, start, cursor, base_offset, None));
            continue;
        }
        cursor += 1;
        tokens.push(vb_token("symbol", index, start, cursor, base_offset, None));
    }
    tokens
}

fn vb_token(
    kind: &str,
    index: &TextIndex<'_>,
    start: usize,
    end: usize,
    base_offset: usize,
    value: Option<String>,
) -> VbToken {
    VbToken {
        kind: kind.to_string(),
        start: base_offset + start,
        end: base_offset + end,
        text: index.slice(start, end).to_string(),
        value,
    }
}

fn vb_keywords() -> &'static HashSet<&'static str> {
    static KEYWORDS: OnceLock<HashSet<&'static str>> = OnceLock::new();
    KEYWORDS.get_or_init(|| {
        [
            "and", "as", "byref", "byval", "call", "case", "class", "const", "dim", "do", "each",
            "else", "elseif", "empty", "end", "exit", "explicit", "false", "for", "function",
            "get", "if", "in", "is", "let", "loop", "me", "mod", "new", "next", "not", "nothing",
            "null", "option", "or", "preserve", "private", "property", "public", "redim", "rem",
            "select", "set", "step", "sub", "then", "to", "true", "until", "wend", "while", "with",
        ]
        .into_iter()
        .collect()
    })
}

fn is_rem_comment_start(index: &TextIndex<'_>, offset: usize) -> bool {
    if !index
        .slice(offset, index.len())
        .to_ascii_lowercase()
        .starts_with("rem")
    {
        return false;
    }
    if is_identifier_part(index.char_at(offset + 3).unwrap_or('\0')) {
        return false;
    }
    let mut cursor = offset;
    while cursor > 0 {
        let previous = index.char_at(cursor - 1).unwrap_or('\0');
        if previous != ' ' && previous != '\t' {
            break;
        }
        cursor -= 1;
    }
    cursor == 0
        || matches!(
            index.char_at(cursor - 1),
            Some('\n') | Some('\r') | Some(':')
        )
}

fn create_block_node(kind: &str, start: &VbToken, name: &VbToken, stack: &[VbNode]) -> VbNode {
    let parent_class = stack
        .iter()
        .rev()
        .find(|node| node.kind == "Class")
        .and_then(|node| node.name_token.as_ref())
        .map(|token| token.text.clone());
    VbNode {
        kind: kind.to_string(),
        start: start.start,
        end: start.end,
        content_start: None,
        content_end: None,
        name_token: Some(name.clone()),
        tokens: vec![start.clone(), name.clone()],
        children: Vec::new(),
        procedure_kind: None,
        property_accessor: None,
        declaration_kind: None,
        visibility: None,
        identifiers: Vec::new(),
        array_declarations: Vec::new(),
        parameters: Vec::new(),
        parameter_metadata: Vec::new(),
        type_name: None,
        member_of: parent_class,
        scope_name: None,
        scope_start: Some(start.start),
        scope_end: Some(start.end),
    }
}

fn create_procedure_node(
    procedure_kind: &str,
    start: &VbToken,
    name: &VbToken,
    parameter_metadata: Vec<Value>,
    stack: &[VbNode],
    property_accessor: Option<String>,
    visibility: Option<String>,
) -> VbNode {
    let parent_class = stack
        .iter()
        .rev()
        .find(|node| node.kind == "Class")
        .and_then(|node| node.name_token.as_ref())
        .map(|token| token.text.clone());
    let parameters = parameter_metadata
        .iter()
        .filter_map(|metadata| metadata.get("token").and_then(value_to_vb_token))
        .collect::<Vec<_>>();
    VbNode {
        kind: if procedure_kind == "property" {
            "Property"
        } else {
            "Procedure"
        }
        .to_string(),
        start: start.start,
        end: start.end,
        content_start: None,
        content_end: None,
        name_token: Some(name.clone()),
        tokens: vec![start.clone(), name.clone()],
        children: Vec::new(),
        procedure_kind: Some(procedure_kind.to_string()),
        property_accessor,
        declaration_kind: None,
        visibility,
        identifiers: Vec::new(),
        array_declarations: Vec::new(),
        parameters,
        parameter_metadata,
        type_name: None,
        member_of: parent_class,
        scope_name: Some(name.text.clone()),
        scope_start: Some(start.start),
        scope_end: Some(start.end),
    }
}

fn collect_parameter_metadata(tokens: &[VbToken], index: usize) -> Vec<Value> {
    let mut parameters = Vec::new();
    if tokens.get(index).map(|token| token.text.as_str()) != Some("(") {
        return parameters;
    }
    let mut cursor = index + 1;
    let mut mode: Option<String> = None;
    let mut mode_explicit = false;
    let mut optional = false;
    let mut can_read_name = true;
    while cursor < tokens.len() && tokens[cursor].text != ")" {
        let token = &tokens[cursor];
        let lower = token.text.to_ascii_lowercase();
        if token.text == "," {
            mode = None;
            mode_explicit = false;
            optional = false;
            can_read_name = true;
        } else if lower == "optional" {
            optional = true;
        } else if lower == "byval" {
            mode = Some("byval".to_string());
            mode_explicit = true;
        } else if lower == "byref" {
            mode = Some("byref".to_string());
            mode_explicit = true;
        } else if can_read_name && token.kind == "identifier" {
            parameters.push(json!({
                "token": vb_token_to_value(token),
                "mode": mode.clone().unwrap_or_else(|| "byref".to_string()),
                "modeExplicit": mode_explicit,
                "optional": optional,
            }));
            can_read_name = false;
        }
        cursor += 1;
    }
    parameters
}

fn create_declaration_node(
    start: &VbToken,
    kind: &str,
    declaration_kind: &str,
    tokens: &[VbToken],
    start_index: usize,
    visibility: Option<String>,
) -> VbNode {
    let end_index = statement_end_index(tokens, start_index.saturating_sub(1));
    let mut identifiers = Vec::new();
    let mut array_declarations = Vec::new();
    let mut can_read_identifier = true;
    for index in start_index..=end_index {
        let Some(current) = tokens.get(index) else {
            continue;
        };
        if current.text == "(" {
            can_read_identifier = false;
        } else if current.text == ")" || current.text == "," {
            can_read_identifier = current.text == ",";
        } else if current.text == "=" {
            break;
        } else if current.kind == "identifier" && can_read_identifier {
            identifiers.push(current.clone());
            if let Some(array) = read_array_declaration(tokens, index, end_index, declaration_kind)
            {
                array_declarations.push(array);
            }
            can_read_identifier = false;
        }
    }
    VbNode {
        kind: kind.to_string(),
        start: start.start,
        end: statement_end(tokens, start_index.saturating_sub(1)),
        content_start: None,
        content_end: None,
        name_token: None,
        tokens: statement_tokens(tokens, start_index.saturating_sub(1)),
        children: Vec::new(),
        procedure_kind: None,
        property_accessor: None,
        declaration_kind: Some(declaration_kind.to_string()),
        visibility,
        identifiers,
        array_declarations,
        parameters: Vec::new(),
        parameter_metadata: Vec::new(),
        type_name: None,
        member_of: None,
        scope_name: None,
        scope_start: None,
        scope_end: None,
    }
}

fn read_array_declaration(
    tokens: &[VbToken],
    identifier_index: usize,
    end_index: usize,
    declaration_kind: &str,
) -> Option<Value> {
    let open_index = identifier_index + 1;
    if tokens.get(open_index).map(|token| token.text.as_str()) != Some("(") {
        return None;
    }
    let mut depth = 0i32;
    let mut close_index = None;
    let mut index = open_index;
    while index <= end_index {
        match tokens.get(index).map(|token| token.text.as_str()) {
            Some("(") => depth += 1,
            Some(")") => {
                depth -= 1;
                if depth == 0 {
                    close_index = Some(index);
                    break;
                }
            }
            _ => {}
        }
        index += 1;
    }
    let close_index = close_index?;
    let dimensions = array_dimension_texts(&tokens[open_index + 1..close_index]);
    let kind = if declaration_kind == "redim" || dimensions.is_empty() {
        "dynamic"
    } else {
        "fixed"
    };
    let name = tokens.get(identifier_index)?;
    Some(json!({
        "name": vb_token_to_value(name),
        "kind": kind,
        "dimensions": dimensions,
    }))
}

// significant トークン列を受け取る前提（空白・コメントは呼び出し前に除去済み）。
// トップレベルのカンマで次元を分割し、各トークンの text を連結して返す。
fn array_dimension_texts(tokens: &[VbToken]) -> Vec<String> {
    fn flush(current: &mut Vec<String>, dimensions: &mut Vec<String>) {
        let text = current.concat();
        let trimmed = text.trim();
        if !trimmed.is_empty() {
            dimensions.push(trimmed.to_string());
        }
        current.clear();
    }
    let mut dimensions = Vec::new();
    let mut current: Vec<String> = Vec::new();
    let mut depth = 0i32;
    for token in tokens {
        if token.text == "(" {
            depth += 1;
        } else if token.text == ")" {
            depth = (depth - 1).max(0);
        }
        if token.text == "," && depth == 0 {
            flush(&mut current, &mut dimensions);
            continue;
        }
        current.push(token.text.clone());
    }
    flush(&mut current, &mut dimensions);
    dimensions
}

fn create_statement_node(
    kind: &str,
    start: &VbToken,
    tokens: &[VbToken],
    start_index: usize,
    name_token: Option<VbToken>,
) -> VbNode {
    let end = statement_end(tokens, start_index);
    VbNode {
        kind: kind.to_string(),
        start: start.start,
        end,
        content_start: None,
        content_end: None,
        name_token,
        tokens: statement_tokens(tokens, start_index),
        children: Vec::new(),
        procedure_kind: None,
        property_accessor: None,
        declaration_kind: None,
        visibility: None,
        identifiers: Vec::new(),
        array_declarations: Vec::new(),
        parameters: Vec::new(),
        parameter_metadata: Vec::new(),
        type_name: None,
        member_of: None,
        scope_name: None,
        scope_start: Some(start.start),
        scope_end: Some(end),
    }
}

fn push_child(document: &mut VbNode, stack: &mut [VbNode], child: VbNode) {
    if let Some(parent) = stack.last_mut() {
        parent.children.push(child);
    } else {
        document.children.push(child);
    }
}

fn close_block(document: &mut VbNode, stack: &mut Vec<VbNode>, end_kind: &str, end: usize) {
    let target = match end_kind {
        "class" => "Class",
        "property" => "Property",
        "with" => "With",
        "if" => "If",
        "select" => "Select",
        "loop" => "DoLoop",
        "wend" => "While",
        "next" => "ForEach",
        _ => "Procedure",
    };
    if let Some(index) = stack.iter().rposition(|node| node.kind == target) {
        let mut node = stack.remove(index);
        node.end = end;
        node.scope_end = Some(end);
        attach_closed_node(document, stack, node);
    }
}

fn close_unclosed(document: &mut VbNode, stack: &mut Vec<VbNode>, end: usize) {
    while let Some(mut node) = stack.pop() {
        node.end = node.end.max(end);
        node.scope_end = Some(node.scope_end.unwrap_or(node.end).max(end));
        attach_closed_node(document, stack, node);
    }
    document.end = document.end.max(end);
}

fn attach_closed_node(document: &mut VbNode, stack: &mut [VbNode], node: VbNode) {
    if let Some(parent) = stack.last_mut() {
        if let Some(existing) = parent
            .children
            .iter_mut()
            .rev()
            .find(|child| child.start == node.start && child.kind == node.kind)
        {
            *existing = node;
            return;
        }
    } else if let Some(existing) = document
        .children
        .iter_mut()
        .rev()
        .find(|child| child.start == node.start && child.kind == node.kind)
    {
        *existing = node;
        return;
    }
}

fn statement_end_index(tokens: &[VbToken], start_index: usize) -> usize {
    let mut index = start_index;
    while index + 1 < tokens.len() {
        let next = &tokens[index + 1];
        if (next.kind == "newline" && tokens[index].text != "_") || next.text == ":" {
            break;
        }
        index += 1;
    }
    index
}

fn statement_end(tokens: &[VbToken], start_index: usize) -> usize {
    tokens
        .get(statement_end_index(tokens, start_index))
        .or_else(|| tokens.get(start_index))
        .map(|token| token.end)
        .unwrap_or(0)
}

fn statement_tokens(tokens: &[VbToken], start_index: usize) -> Vec<VbToken> {
    let end = statement_end_index(tokens, start_index);
    tokens.get(start_index..=end).unwrap_or_default().to_vec()
}

fn is_statement_start(tokens: &[VbToken], index: usize) -> bool {
    index == 0
        || tokens
            .get(index.saturating_sub(1))
            .map(|token| token.kind == "newline" || token.text == ":")
            .unwrap_or(true)
}

fn is_multiline_if(tokens: &[VbToken], start_index: usize) -> bool {
    let end_index = statement_end_index(tokens, start_index);
    find_keyword(tokens, start_index, end_index, "then") == Some(end_index)
}

fn statement_has_symbol(tokens: &[VbToken], start_index: usize, symbol: &str) -> bool {
    let end = statement_end_index(tokens, start_index);
    tokens[start_index..=end]
        .iter()
        .any(|token| token.text == symbol)
}

fn find_keyword(tokens: &[VbToken], start: usize, end: usize, keyword: &str) -> Option<usize> {
    (start..=end).find(|index| lower_token(tokens.get(*index)) == keyword)
}

fn find_create_object_call(tokens: &[VbToken], start: usize, end: usize) -> Option<usize> {
    for index in start..=end {
        if lower_token(tokens.get(index)) == "createobject" {
            return Some(index);
        }
        if index + 2 <= end
            && lower_token(tokens.get(index)) == "server"
            && tokens.get(index + 1).map(|token| token.text.as_str()) == Some(".")
            && lower_token(tokens.get(index + 2)) == "createobject"
        {
            return Some(index);
        }
    }
    None
}

fn vb_node_to_value(node: &VbNode) -> Value {
    let mut object = JsonMap::new();
    object.insert("kind".to_string(), Value::String(node.kind.clone()));
    object.insert("start".to_string(), json!(node.start));
    object.insert("end".to_string(), json!(node.end));
    if let Some(value) = node.content_start {
        object.insert("contentStart".to_string(), json!(value));
    }
    if let Some(value) = node.content_end {
        object.insert("contentEnd".to_string(), json!(value));
    }
    if let Some(token) = &node.name_token {
        object.insert("nameToken".to_string(), vb_token_to_value(token));
    }
    object.insert(
        "tokens".to_string(),
        Value::Array(node.tokens.iter().map(vb_token_to_value).collect()),
    );
    object.insert(
        "children".to_string(),
        Value::Array(node.children.iter().map(vb_node_to_value).collect()),
    );
    insert_optional_string(&mut object, "procedureKind", &node.procedure_kind);
    insert_optional_string(&mut object, "propertyAccessor", &node.property_accessor);
    insert_optional_string(&mut object, "declarationKind", &node.declaration_kind);
    insert_optional_string(&mut object, "visibility", &node.visibility);
    if !node.identifiers.is_empty() {
        object.insert(
            "identifiers".to_string(),
            Value::Array(node.identifiers.iter().map(vb_token_to_value).collect()),
        );
    }
    if !node.array_declarations.is_empty() {
        object.insert(
            "arrayDeclarations".to_string(),
            Value::Array(node.array_declarations.clone()),
        );
    }
    if !node.parameters.is_empty() {
        object.insert(
            "parameters".to_string(),
            Value::Array(node.parameters.iter().map(vb_token_to_value).collect()),
        );
    }
    if !node.parameter_metadata.is_empty() {
        object.insert(
            "parameterMetadata".to_string(),
            Value::Array(node.parameter_metadata.clone()),
        );
    }
    insert_optional_string(&mut object, "typeName", &node.type_name);
    insert_optional_string(&mut object, "memberOf", &node.member_of);
    insert_optional_string(&mut object, "scopeName", &node.scope_name);
    if let Some(value) = node.scope_start {
        object.insert("scopeStart".to_string(), json!(value));
    }
    if let Some(value) = node.scope_end {
        object.insert("scopeEnd".to_string(), json!(value));
    }
    Value::Object(object)
}

fn vb_token_to_value(token: &VbToken) -> Value {
    let mut object = JsonMap::new();
    object.insert("kind".to_string(), Value::String(token.kind.clone()));
    object.insert("start".to_string(), json!(token.start));
    object.insert("end".to_string(), json!(token.end));
    object.insert("text".to_string(), Value::String(token.text.clone()));
    if let Some(value) = &token.value {
        object.insert("value".to_string(), Value::String(value.clone()));
    }
    Value::Object(object)
}

fn value_to_vb_token(value: &Value) -> Option<VbToken> {
    Some(VbToken {
        kind: value.get("kind")?.as_str()?.to_string(),
        start: value.get("start")?.as_u64()? as usize,
        end: value.get("end")?.as_u64()? as usize,
        text: value.get("text")?.as_str()?.to_string(),
        value: value
            .get("value")
            .and_then(Value::as_str)
            .map(str::to_string),
    })
}

fn collect_symbols_from_parsed(parsed: &Value, context: &Value) -> Vec<Value> {
    let uri = parsed
        .get("uri")
        .and_then(Value::as_str)
        .unwrap_or_default();
    let text = parsed
        .get("text")
        .and_then(Value::as_str)
        .unwrap_or_default();
    let text_index = TextIndex::new(text);
    let mut symbols = Vec::new();
    if let Some(regions) = parsed.get("regions").and_then(Value::as_array) {
        for region in regions {
            if region.get("language").and_then(Value::as_str) != Some("vbscript") {
                continue;
            }
            let start = region
                .get("contentStart")
                .and_then(Value::as_u64)
                .unwrap_or(0) as usize;
            let end = region
                .get("contentEnd")
                .and_then(Value::as_u64)
                .unwrap_or(0) as usize;
            let cst = parse_vbscript_cst(text_index.slice(start, end), text, start);
            let empty_tokens = Vec::new();
            let document_tokens = cst
                .get("tokens")
                .and_then(Value::as_array)
                .unwrap_or(&empty_tokens);
            collect_symbols_from_node(
                &cst,
                uri,
                &text_index,
                &document_tokens,
                None,
                None,
                None,
                &mut symbols,
            );
        }
    }
    add_server_object_symbols(parsed, &mut symbols);
    apply_type_annotations(parsed, &text_index, &mut symbols);
    add_implicit_assignment_symbols(parsed, &text_index, context, &mut symbols);
    strip_null_fields_from_values(&mut symbols);
    symbols
}

fn strip_null_fields_from_values(values: &mut [Value]) {
    for value in values {
        strip_null_fields(value);
    }
}

fn strip_null_fields(value: &mut Value) {
    match value {
        Value::Object(object) => {
            object.retain(|_, nested| {
                strip_null_fields(nested);
                !nested.is_null()
            });
        }
        Value::Array(items) => {
            for item in items {
                strip_null_fields(item);
            }
        }
        _ => {}
    }
}

fn collect_symbols_from_node(
    node: &Value,
    uri: &str,
    text_index: &TextIndex<'_>,
    document_tokens: &[Value],
    member_of: Option<String>,
    scope_name: Option<String>,
    scope_range: Option<Value>,
    symbols: &mut Vec<Value>,
) {
    let kind = node.get("kind").and_then(Value::as_str).unwrap_or_default();
    let mut current_member = member_of.clone();
    let mut current_scope = scope_name.clone();
    if kind == "Class" {
        if let Some(name) = node.get("nameToken").and_then(token_name) {
            let range = token_range(text_index, node.get("nameToken").unwrap());
            let mut symbol = json!({
                "name": name,
                "kind": "class",
                "range": range,
                "sourceUri": uri,
                "scopeRange": text_index.range(value_usize(node, "start"), value_usize(node, "end")),
            });
            if let Some(documentation) = documentation_for_node(node, document_tokens) {
                symbol["documentation"] = documentation;
            }
            symbols.push(symbol);
            current_member = Some(name.to_string());
        }
    } else if kind == "Procedure" || kind == "Property" {
        if let Some(name) = node.get("nameToken").and_then(token_name) {
            let procedure_kind = node
                .get("procedureKind")
                .and_then(Value::as_str)
                .unwrap_or("sub");
            let symbol_kind = if kind == "Property" {
                "property"
            } else if member_of.is_some() {
                "method"
            } else if procedure_kind == "function" {
                "function"
            } else {
                "sub"
            };
            let mut symbol = JsonMap::new();
            symbol.insert("name".to_string(), Value::String(name.to_string()));
            symbol.insert("kind".to_string(), Value::String(symbol_kind.to_string()));
            symbol.insert(
                "range".to_string(),
                token_range(text_index, node.get("nameToken").unwrap()),
            );
            symbol.insert("sourceUri".to_string(), Value::String(uri.to_string()));
            if let Some(owner) = &member_of {
                symbol.insert("memberOf".to_string(), Value::String(owner.clone()));
                symbol.insert("containerName".to_string(), Value::String(owner.clone()));
            }
            symbol.insert(
                "scopeRange".to_string(),
                text_index.range(value_usize(node, "start"), value_usize(node, "end")),
            );
            if let Some(visibility) = node.get("visibility").and_then(Value::as_str) {
                symbol.insert(
                    "visibility".to_string(),
                    Value::String(visibility.to_string()),
                );
            }
            if let Some(accessor) = node.get("propertyAccessor").and_then(Value::as_str) {
                symbol.insert(
                    "propertyAccessor".to_string(),
                    Value::String(accessor.to_string()),
                );
            }
            let parameters = node
                .get("parameters")
                .and_then(Value::as_array)
                .map(|items| {
                    items
                        .iter()
                        .filter_map(token_name)
                        .map(|name| Value::String(name.to_string()))
                        .collect::<Vec<_>>()
                })
                .unwrap_or_default();
            if !parameters.is_empty() {
                symbol.insert("parameters".to_string(), Value::Array(parameters));
            }
            if let Some(parameter_details) = node.get("parameterMetadata").cloned() {
                symbol.insert("parameterDetails".to_string(), parameter_details);
            }
            if procedure_kind == "function" || procedure_kind == "sub" {
                symbol.insert(
                    "procedureKind".to_string(),
                    Value::String(procedure_kind.to_string()),
                );
            }
            if let Some(documentation) = documentation_for_node(node, document_tokens) {
                symbol.insert("documentation".to_string(), documentation);
            }
            symbols.push(Value::Object(symbol));
            current_scope = Some(name.to_string());
            let current_scope_range =
                Some(text_index.range(value_usize(node, "start"), value_usize(node, "end")));
            if let Some(parameters) = node.get("parameterMetadata").and_then(Value::as_array) {
                for parameter in parameters {
                    if let Some(token) = parameter.get("token") {
                        if let Some(name) = token_name(token) {
                            symbols.push(json!({
                                "name": name,
                                "kind": "parameter",
                                "range": token_range(text_index, token),
                                "sourceUri": uri,
                                "scopeName": current_scope,
                                "scopeRange": current_scope_range,
                                "parameterMode": parameter.get("mode").and_then(Value::as_str).unwrap_or("byref"),
                                "optional": parameter.get("optional").and_then(Value::as_bool).unwrap_or(false),
                            }));
                        }
                    }
                }
            }
        }
    } else if kind == "VariableDeclaration" || kind == "ConstantDeclaration" || kind == "ForEach" {
        if let Some(identifiers) = node.get("identifiers").and_then(Value::as_array) {
            for token in identifiers {
                if let Some(name) = token_name(token) {
                    let array_declaration = array_declaration_for_token(node, token);
                    let symbol_member_of = if scope_name.is_some() {
                        None
                    } else {
                        member_of.clone()
                    };
                    let symbol_kind = if kind == "ConstantDeclaration" {
                        "constant"
                    } else if symbol_member_of.is_some() {
                        "field"
                    } else {
                        "variable"
                    };
                    let local_scope_range = scope_range.clone().or_else(|| {
                        symbol_member_of.as_ref().map(|_| {
                            text_index.range(value_usize(node, "start"), value_usize(node, "end"))
                        })
                    });
                    let mut symbol = json!({
                        "name": name,
                        "kind": symbol_kind,
                        "range": token_range(text_index, token),
                        "sourceUri": uri,
                        "memberOf": symbol_member_of,
                        "containerName": symbol_member_of,
                        "scopeName": scope_name,
                        "scopeRange": local_scope_range,
                        "visibility": node.get("visibility").and_then(Value::as_str),
                        "documentation": documentation_for_node(node, document_tokens),
                    });
                    if let Some(array) = array_declaration {
                        if let Some(object) = symbol.as_object_mut() {
                            object
                                .insert("typeName".to_string(), Value::String("Array".to_string()));
                            object.insert("type".to_string(), json!({ "name": "Array" }));
                            object.insert("explicitType".to_string(), Value::Bool(true));
                            object.insert(
                                "array".to_string(),
                                json!({
                                    "kind": array
                                        .get("kind")
                                        .and_then(Value::as_str)
                                        .unwrap_or("dynamic"),
                                    "dimensions": array
                                        .get("dimensions")
                                        .cloned()
                                        .unwrap_or_else(|| Value::Array(Vec::new())),
                                }),
                            );
                        }
                    }
                    symbols.push(symbol);
                }
            }
        }
    }
    if let Some(children) = node.get("children").and_then(Value::as_array) {
        for child in children {
            collect_symbols_from_node(
                child,
                uri,
                text_index,
                document_tokens,
                current_member.clone(),
                current_scope.clone(),
                scope_range.clone().or_else(|| {
                    if kind == "Procedure" || kind == "Property" {
                        Some(text_index.range(value_usize(node, "start"), value_usize(node, "end")))
                    } else {
                        None
                    }
                }),
                symbols,
            );
        }
    }
}

fn array_declaration_for_token<'a>(node: &'a Value, token: &Value) -> Option<&'a Value> {
    let token_start = value_usize(token, "start");
    let token_end = value_usize(token, "end");
    node.get("arrayDeclarations")
        .and_then(Value::as_array)?
        .iter()
        .find(|array| {
            array.get("name").is_some_and(|name| {
                value_usize(name, "start") == token_start && value_usize(name, "end") == token_end
            })
        })
}

fn documentation_for_node(node: &Value, document_tokens: &[Value]) -> Option<Value> {
    let offset = value_usize(node, "start");
    let mut index = document_tokens
        .iter()
        .position(|token| value_usize(token, "start") >= offset)
        .unwrap_or(document_tokens.len());
    while index > 0 && is_whitespace_or_newline_value(&document_tokens[index - 1]) {
        index -= 1;
    }
    let mut comments = Vec::new();
    while index > 0 {
        let token = &document_tokens[index - 1];
        if token.get("kind").and_then(Value::as_str) != Some("comment") {
            break;
        }
        comments.push(
            token
                .get("text")
                .and_then(Value::as_str)
                .unwrap_or_default(),
        );
        index -= 1;
        while index > 0 && is_whitespace_or_newline_value(&document_tokens[index - 1]) {
            index -= 1;
        }
    }
    comments.reverse();
    if comments.is_empty() {
        return None;
    }
    if comments.iter().all(|comment| comment.starts_with("'''")) {
        return xml_documentation(&comments);
    }
    plain_documentation(&comments)
}

fn is_whitespace_or_newline_value(token: &Value) -> bool {
    matches!(
        token.get("kind").and_then(Value::as_str),
        Some("whitespace" | "newline")
    )
}

fn plain_documentation(comments: &[&str]) -> Option<Value> {
    let summary = comments
        .iter()
        .map(|comment| {
            comment
                .strip_prefix('\'')
                .unwrap_or(comment)
                .trim_end()
                .to_string()
        })
        .filter(|line| !line.trim_start().starts_with('@'))
        .collect::<Vec<_>>()
        .join("\n")
        .trim()
        .to_string();
    if summary.is_empty() {
        return None;
    }
    Some(json!({
        "format": "plain",
        "summary": summary,
        "params": {},
        "exceptions": [],
        "see": [],
        "seealso": [],
    }))
}

fn xml_documentation(comments: &[&str]) -> Option<Value> {
    let xml = comments
        .iter()
        .map(|comment| comment.strip_prefix("'''").unwrap_or(comment).trim())
        .collect::<Vec<_>>()
        .join("\n");
    let summary = xml_tag_text(&xml, "summary");
    let remarks = xml_tag_text(&xml, "remarks");
    let returns = xml_tag_text(&xml, "returns");
    let value = xml_tag_text(&xml, "value");
    let example = xml_tag_text(&xml, "example");
    let code = xml_tag_text(&xml, "code");
    let params = xml_param_texts(&xml);
    if summary.is_none()
        && remarks.is_none()
        && returns.is_none()
        && value.is_none()
        && example.is_none()
        && code.is_none()
        && params.is_empty()
    {
        return None;
    }
    Some(json!({
        "format": "xml",
        "summary": summary,
        "remarks": remarks,
        "returns": returns,
        "value": value,
        "example": example,
        "code": code,
        "params": params,
        "exceptions": [],
        "see": [],
        "seealso": [],
    }))
}

fn xml_tag_text(xml: &str, tag: &str) -> Option<String> {
    let open = format!("<{tag}>");
    let close = format!("</{tag}>");
    let start = xml.find(&open)? + open.len();
    let end = xml[start..].find(&close)? + start;
    Some(xml[start..end].trim().to_string()).filter(|value| !value.is_empty())
}

fn xml_param_texts(xml: &str) -> JsonMap {
    let mut params = JsonMap::new();
    let mut cursor = 0usize;
    while let Some(param_start) = xml[cursor..].find("<param") {
        let start = cursor + param_start;
        let Some(open_end) = xml[start..].find('>') else {
            break;
        };
        let open_end = start + open_end;
        let open = &xml[start..=open_end];
        let Some(name_start) = open.find("name=\"").map(|offset| offset + "name=\"".len()) else {
            cursor = open_end + 1;
            continue;
        };
        let Some(name_end) = open[name_start..]
            .find('"')
            .map(|offset| name_start + offset)
        else {
            cursor = open_end + 1;
            continue;
        };
        let Some(close_start) = xml[open_end + 1..]
            .find("</param>")
            .map(|offset| open_end + 1 + offset)
        else {
            cursor = open_end + 1;
            continue;
        };
        let name = open[name_start..name_end].trim();
        let text = xml[open_end + 1..close_start].trim();
        if !name.is_empty() && !text.is_empty() {
            params.insert(name.to_string(), Value::String(text.to_string()));
        }
        cursor = close_start + "</param>".len();
    }
    params
}

fn add_server_object_symbols(parsed: &Value, symbols: &mut Vec<Value>) {
    let Some(objects) = parsed.get("serverObjects").and_then(Value::as_array) else {
        return;
    };
    let mut existing = symbols
        .iter()
        .filter_map(|symbol| symbol.get("name").and_then(Value::as_str))
        .map(|name| name.to_ascii_lowercase())
        .collect::<HashSet<_>>();
    for object in objects {
        let Some(id) = object.get("id").and_then(Value::as_str) else {
            continue;
        };
        if !is_vb_identifier(id) || existing.contains(&id.to_ascii_lowercase()) {
            continue;
        }
        existing.insert(id.to_ascii_lowercase());
        let mut symbol = JsonMap::new();
        symbol.insert("name".to_string(), Value::String(id.to_string()));
        symbol.insert("kind".to_string(), Value::String("variable".to_string()));
        symbol.insert(
            "range".to_string(),
            object.get("idRange").cloned().unwrap_or_else(
                || json!({"start":{"line":0,"character":0},"end":{"line":0,"character":0}}),
            ),
        );
        symbol.insert(
            "sourceUri".to_string(),
            parsed
                .get("uri")
                .cloned()
                .unwrap_or_else(|| Value::String(String::new())),
        );
        if let Some(prog_id) = object.get("progId").and_then(Value::as_str) {
            symbol.insert("typeName".to_string(), Value::String(prog_id.to_string()));
            symbol.insert(
                "type".to_string(),
                json!({ "name": prog_id, "object": true }),
            );
            symbol.insert("explicitType".to_string(), Value::Bool(true));
        }
        symbols.push(Value::Object(symbol));
    }
}

fn add_implicit_assignment_symbols(
    parsed: &Value,
    text_index: &TextIndex<'_>,
    context: &Value,
    symbols: &mut Vec<Value>,
) {
    let text = parsed
        .get("text")
        .and_then(Value::as_str)
        .unwrap_or_default();
    if text.to_ascii_lowercase().contains("option explicit") {
        return;
    }
    let uri = parsed
        .get("uri")
        .and_then(Value::as_str)
        .unwrap_or_default();
    let mut existing = symbols
        .iter()
        .chain(
            context
                .get("symbols")
                .and_then(Value::as_array)
                .into_iter()
                .flatten(),
        )
        .filter_map(|symbol| symbol.get("name").and_then(Value::as_str))
        .map(|name| name.to_ascii_lowercase())
        .collect::<HashSet<_>>();
    if let Some(regions) = parsed.get("regions").and_then(Value::as_array) {
        for region in regions {
            if region.get("language").and_then(Value::as_str) != Some("vbscript") {
                continue;
            }
            let start = region
                .get("contentStart")
                .and_then(Value::as_u64)
                .unwrap_or(0) as usize;
            let end = region
                .get("contentEnd")
                .and_then(Value::as_u64)
                .unwrap_or(0) as usize;
            let region_index = TextIndex::new(text_index.slice(start, end));
            let tokens = tokenize_vbscript(&region_index, start);
            let significant = tokens
                .iter()
                .filter(|token| {
                    token.kind != "whitespace" && token.kind != "comment" && token.kind != "newline"
                })
                .cloned()
                .collect::<Vec<_>>();
            let mut index = 0usize;
            while index < significant.len() {
                let statement_end = statement_end_index(&significant, index);
                let statement = &significant[index..=statement_end];
                let target_index = if lower_token(statement.first()) == "set" {
                    1
                } else {
                    0
                };
                let Some(target) = statement.get(target_index) else {
                    index = statement_end + 1;
                    continue;
                };
                let has_assignment = statement
                    .iter()
                    .enumerate()
                    .any(|(item_index, token)| item_index > target_index && token.text == "=");
                if target.kind == "identifier"
                    && has_assignment
                    && statement
                        .get(target_index + 1)
                        .map(|token| token.text.as_str())
                        != Some(".")
                    && !is_builtin_name(&target.text)
                    && !is_implicit_keyword_name(&target.text)
                    && !existing.contains(&target.text.to_ascii_lowercase())
                {
                    existing.insert(target.text.to_ascii_lowercase());
                    symbols.push(json!({
                        "name": target.text,
                        "kind": "variable",
                        "range": text_index.range(target.start, target.end),
                        "sourceUri": uri,
                        "implicit": true,
                    }));
                }
                index = statement_end + 1;
            }
        }
    }
}

fn apply_type_annotations(parsed: &Value, text_index: &TextIndex<'_>, symbols: &mut [Value]) {
    let Some(regions) = parsed.get("regions").and_then(Value::as_array) else {
        return;
    };
    for region in regions {
        if region.get("language").and_then(Value::as_str) != Some("vbscript") {
            continue;
        }
        let start = region
            .get("contentStart")
            .and_then(Value::as_u64)
            .unwrap_or(0) as usize;
        let end = region
            .get("contentEnd")
            .and_then(Value::as_u64)
            .unwrap_or(0) as usize;
        let region_index = TextIndex::new(text_index.slice(start, end));
        for token in tokenize_vbscript(&region_index, start)
            .into_iter()
            .filter(|token| token.kind == "comment")
        {
            let text = token.text.trim_start_matches('\'').trim();
            if let Some((name, type_name)) = parse_named_type_annotation(text, "@type") {
                set_annotated_symbol_type(symbols, &name, &type_name, None);
                continue;
            }
            if let Some((name, type_name)) = parse_named_type_annotation(text, "@param") {
                set_annotated_symbol_type(symbols, &name, &type_name, Some("parameter"));
                continue;
            }
            if let Some(type_name) = text
                .strip_prefix("@returns")
                .map(str::trim)
                .filter(|value| !value.is_empty())
            {
                if let Some(next_name) = next_procedure_name(parsed, token.start) {
                    set_annotated_symbol_type(symbols, &next_name, type_name, None);
                }
            }
        }
    }
}

fn parse_named_type_annotation(text: &str, marker: &str) -> Option<(String, String)> {
    let rest = text.strip_prefix(marker)?.trim();
    let (name, type_name) = rest.split_once(" As ")?;
    let name = name.rsplit('.').next().unwrap_or(name).trim().to_string();
    let type_name = type_name.trim().to_string();
    (!name.is_empty() && !type_name.is_empty()).then_some((name, type_name))
}

fn set_annotated_symbol_type(
    symbols: &mut [Value],
    name: &str,
    type_name: &str,
    kind: Option<&str>,
) {
    for symbol in symbols {
        if symbol.get("name").and_then(Value::as_str) != Some(name) {
            continue;
        }
        if kind.is_some() && symbol.get("kind").and_then(Value::as_str) != kind {
            continue;
        }
        if let Some(object) = symbol.as_object_mut() {
            object.insert("typeName".to_string(), Value::String(type_name.to_string()));
            object.insert("type".to_string(), json!({ "name": type_name }));
            object.insert("explicitType".to_string(), Value::Bool(true));
        }
    }
}

fn next_procedure_name(parsed: &Value, offset: usize) -> Option<String> {
    let text = parsed
        .get("text")
        .and_then(Value::as_str)
        .unwrap_or_default();
    let index = TextIndex::new(text);
    let tail_index = TextIndex::new(index.slice(offset, index.len()));
    let tokens = tokenize_vbscript(&tail_index, offset);
    for window in tokens.windows(2) {
        let first = lower_token(window.first());
        let second = window.get(1);
        if (first == "function" || first == "sub")
            && second.map(|token| token.kind.as_str()) == Some("identifier")
        {
            return second.map(|token| token.text.clone());
        }
    }
    None
}

fn diagnose_vbscript(parsed: &Value, symbols: &[Value], context: &Value) -> Vec<Value> {
    let mut diagnostics = Vec::new();
    let text = parsed
        .get("text")
        .and_then(Value::as_str)
        .unwrap_or_default();
    let text_index = TextIndex::new(text);
    let has_option_explicit = text.to_ascii_lowercase().contains("option explicit");
    let declared = symbols
        .iter()
        .chain(
            context
                .get("symbols")
                .and_then(Value::as_array)
                .into_iter()
                .flatten(),
        )
        .filter_map(|symbol| symbol.get("name").and_then(Value::as_str))
        .map(|name| name.to_ascii_lowercase())
        .collect::<HashSet<_>>();
    let declaration_ranges = symbols
        .iter()
        .filter_map(|symbol| symbol.get("range"))
        .filter_map(range_tuple)
        .collect::<HashSet<_>>();
    let mut used = HashSet::<String>::new();
    if let Some(regions) = parsed.get("regions").and_then(Value::as_array) {
        for region in regions {
            if region.get("language").and_then(Value::as_str) != Some("vbscript") {
                continue;
            }
            let start = region
                .get("contentStart")
                .and_then(Value::as_u64)
                .unwrap_or(0) as usize;
            let end = region
                .get("contentEnd")
                .and_then(Value::as_u64)
                .unwrap_or(0) as usize;
            let region_index = TextIndex::new(text_index.slice(start, end));
            let tokens = tokenize_vbscript(&region_index, start);
            for (index, token) in tokens.iter().enumerate() {
                if token.kind != "identifier" {
                    continue;
                }
                if previous_significant_token(&tokens, index).map(|token| token.text.as_str())
                    == Some(".")
                {
                    continue;
                }
                let lower = token.text.to_ascii_lowercase();
                let token_range = text_index.range(token.start, token.end);
                if range_tuple(&token_range)
                    .map(|range| !declaration_ranges.contains(&range))
                    .unwrap_or(true)
                {
                    used.insert(lower.clone());
                }
                if !has_option_explicit {
                    continue;
                }
                if declared.contains(&lower) || is_builtin_name(&token.text) {
                    continue;
                }
                diagnostics.push(json!({
                    "severity": 1,
                    "range": text_index.range(token.start, token.end),
                    "message": format!("'{name}' is not declared.", name = token.text),
                    "source": "asp-lsp",
                    "code": "vbscript:undeclared",
                }));
            }
        }
    }
    if context
        .get("unusedDiagnostics")
        .and_then(Value::as_bool)
        .unwrap_or(true)
    {
        for symbol in symbols {
            let Some(name) = symbol.get("name").and_then(Value::as_str) else {
                continue;
            };
            let kind = symbol
                .get("kind")
                .and_then(Value::as_str)
                .unwrap_or_default();
            if !matches!(
                kind,
                "variable" | "constant" | "parameter" | "function" | "sub" | "method" | "property"
            ) || symbol.get("implicit").and_then(Value::as_bool) == Some(true)
            {
                continue;
            }
            if used.contains(&name.to_ascii_lowercase()) {
                continue;
            }
            let Some(range) = symbol.get("range") else {
                continue;
            };
            diagnostics.push(json!({
                "severity": 4,
                "range": range,
                "message": format!("'{name}' is declared but never used."),
                "source": "asp-lsp-vbscript-unused",
                "code": "vbscript:unused",
                "tags": [1],
                "data": {
                    "kind": kind,
                    "name": name,
                },
            }));
        }
    }
    diagnostics
}

fn range_key(range: &Value) -> String {
    serde_json::to_string(range).unwrap_or_default()
}

fn range_tuple(range: &Value) -> Option<(u64, u64, u64, u64)> {
    Some((
        range.get("start")?.get("line")?.as_u64()?,
        range.get("start")?.get("character")?.as_u64()?,
        range.get("end")?.get("line")?.as_u64()?,
        range.get("end")?.get("character")?.as_u64()?,
    ))
}

fn previous_significant_token(tokens: &[VbToken], index: usize) -> Option<&VbToken> {
    tokens[..index].iter().rev().find(|token| {
        token.kind != "whitespace" && token.kind != "comment" && token.kind != "newline"
    })
}

fn next_significant_token(tokens: &[VbToken], index: usize) -> Option<&VbToken> {
    tokens[index + 1..].iter().find(|token| {
        token.kind != "whitespace" && token.kind != "comment" && token.kind != "newline"
    })
}

fn collect_external_refs(parsed: &Value, symbols: &[Value]) -> Vec<Value> {
    let text = parsed
        .get("text")
        .and_then(Value::as_str)
        .unwrap_or_default();
    let text_index = TextIndex::new(text);
    let declared = symbols
        .iter()
        .filter_map(|symbol| symbol.get("name").and_then(Value::as_str))
        .map(|name| name.to_ascii_lowercase())
        .collect::<HashSet<_>>();
    let declaration_ranges = symbols
        .iter()
        .filter_map(|symbol| symbol.get("range"))
        .filter_map(range_tuple)
        .collect::<HashSet<_>>();
    let mut refs = Vec::new();
    let mut seen = HashSet::new();
    if let Some(regions) = parsed.get("regions").and_then(Value::as_array) {
        for region in regions {
            if region.get("language").and_then(Value::as_str) != Some("vbscript") {
                continue;
            }
            let start = region
                .get("contentStart")
                .and_then(Value::as_u64)
                .unwrap_or(0) as usize;
            let end = region
                .get("contentEnd")
                .and_then(Value::as_u64)
                .unwrap_or(0) as usize;
            let region_index = TextIndex::new(text_index.slice(start, end));
            let tokens = tokenize_vbscript(&region_index, start);
            for (index, token) in tokens.iter().enumerate() {
                if token.kind != "identifier" {
                    continue;
                }
                if previous_significant_token(&tokens, index).map(|token| token.text.as_str())
                    == Some(".")
                {
                    continue;
                }
                let lower = token.text.to_ascii_lowercase();
                let token_range = text_index.range(token.start, token.end);
                if range_tuple(&token_range)
                    .map(|range| declaration_ranges.contains(&range))
                    .unwrap_or(false)
                    || declared.contains(&lower)
                    || is_builtin_name(&token.text)
                {
                    continue;
                }
                let next = next_significant_token(&tokens, index);
                let member_name = if next.map(|token| token.text.as_str()) == Some(".") {
                    next_significant_token(&tokens, index + 1)
                        .filter(|token| token.kind == "identifier")
                        .map(|token| token.text.clone())
                } else {
                    None
                };
                let key = format!(
                    "{}|{}|{}",
                    lower,
                    member_name
                        .as_ref()
                        .map(|value| value.to_ascii_lowercase())
                        .unwrap_or_default(),
                    range_key(&token_range)
                );
                if !seen.insert(key) {
                    continue;
                }
                refs.push(json!({
                    "name": token.text,
                    "range": token_range,
                    "kindHint": if next.map(|token| token.text.as_str()) == Some("(") { Value::String("function".to_string()) } else { Value::Null },
                    "memberName": member_name,
                }));
            }
        }
    }
    refs
}

fn external_ref_usages(refs: &[Value]) -> Vec<Value> {
    let mut usages: JsonMap = JsonMap::new();
    for reference in refs {
        let Some(name) = reference.get("name").and_then(Value::as_str) else {
            continue;
        };
        let member = reference.get("memberName").and_then(Value::as_str);
        let key = member
            .map(|member| {
                format!(
                    "{}.{}",
                    name.to_ascii_lowercase(),
                    member.to_ascii_lowercase()
                )
            })
            .unwrap_or_else(|| name.to_ascii_lowercase());
        let entry = usages.entry(key.clone()).or_insert_with(|| {
            json!({
                "key": key,
                "name": name,
                "memberName": member,
                "kindHint": reference.get("kindHint").cloned().unwrap_or(Value::Null),
                "count": 0,
                "ranges": [],
            })
        });
        if let Some(object) = entry.as_object_mut() {
            let count = object.get("count").and_then(Value::as_u64).unwrap_or(0) + 1;
            object.insert("count".to_string(), json!(count));
            if let Some(ranges) = object.get_mut("ranges").and_then(Value::as_array_mut) {
                ranges.push(reference.get("range").cloned().unwrap_or_else(
                    || json!({"start":{"line":0,"character":0},"end":{"line":0,"character":0}}),
                ));
            }
        }
    }
    usages.into_values().collect()
}

fn summarize_asp_file(parsed: &Value) -> Value {
    let uri = parsed
        .get("uri")
        .and_then(Value::as_str)
        .unwrap_or_default();
    let text = parsed
        .get("text")
        .and_then(Value::as_str)
        .unwrap_or_default();
    let regions = parsed
        .get("regions")
        .and_then(Value::as_array)
        .cloned()
        .unwrap_or_default();
    let language_regions = regions
        .iter()
        .map(|region| {
            let start = region.get("contentStart").and_then(Value::as_u64).unwrap_or(0) as usize;
            let end = region.get("contentEnd").and_then(Value::as_u64).unwrap_or(0) as usize;
            json!({
                "language": region.get("language").cloned().unwrap_or(Value::String("html".to_string())),
                "kind": region.get("kind").cloned().unwrap_or(Value::String("html".to_string())),
                "start": region.get("start").cloned().unwrap_or(json!(0)),
                "end": region.get("end").cloned().unwrap_or(json!(0)),
                "contentStart": region.get("contentStart").cloned().unwrap_or(json!(0)),
                "contentEnd": region.get("contentEnd").cloned().unwrap_or(json!(0)),
                "fingerprint": fingerprint(text.get(start..end).unwrap_or_default()),
            })
        })
        .collect::<Vec<_>>();
    let symbols = collect_symbols_from_parsed(parsed, &Value::Null);
    let public_symbols = symbols
        .iter()
        .filter(|symbol| symbol.get("visibility").and_then(Value::as_str) != Some("private"))
        .cloned()
        .collect::<Vec<_>>();
    let exports = public_symbols
        .iter()
        .map(|symbol| {
            json!({
                "name": symbol.get("name").cloned().unwrap_or(Value::String(String::new())),
                "kind": symbol.get("kind").cloned().unwrap_or(Value::String("variable".to_string())),
                "range": symbol.get("range").cloned().unwrap_or(json!({"start":{"line":0,"character":0},"end":{"line":0,"character":0}})),
                "memberOf": symbol.get("memberOf").cloned().unwrap_or(Value::Null),
                "visibility": symbol.get("visibility").cloned().unwrap_or(Value::Null),
            })
        })
        .collect::<Vec<_>>();
    let external_refs = collect_external_refs(parsed, &symbols);
    let external_ref_usages = external_ref_usages(&external_refs);
    let vbscript = if regions
        .iter()
        .any(|region| region.get("language").and_then(Value::as_str) == Some("vbscript"))
        || parsed
            .get("serverObjects")
            .and_then(Value::as_array)
            .map(|objects| !objects.is_empty())
            .unwrap_or(false)
    {
        Some(json!({
            "fingerprint": fingerprint(text),
            "localSymbols": symbols,
            "publicSymbols": public_symbols,
            "exports": exports,
            "externalRefs": external_refs,
            "externalRefUsages": external_ref_usages,
            "typeFacts": [],
        }))
    } else {
        None
    };
    let mut result = JsonMap::new();
    result.insert("uri".to_string(), Value::String(uri.to_string()));
    result.insert("fingerprint".to_string(), Value::String(fingerprint(text)));
    result.insert(
        "defaultLanguage".to_string(),
        parsed
            .get("defaultLanguage")
            .cloned()
            .unwrap_or(Value::String("VBScript".to_string())),
    );
    result.insert(
        "languageRegions".to_string(),
        Value::Array(language_regions),
    );
    result.insert(
        "includeRefs".to_string(),
        parsed
            .get("includes")
            .cloned()
            .unwrap_or_else(|| Value::Array(Vec::new())),
    );
    result.insert(
        "diagnostics".to_string(),
        parsed
            .get("diagnostics")
            .cloned()
            .unwrap_or_else(|| Value::Array(Vec::new())),
    );
    if let Some(vbscript) = vbscript {
        result.insert("vbscript".to_string(), vbscript);
    }
    Value::Object(result)
}

fn fingerprint(text: &str) -> String {
    let mut hash: u64 = 0xcbf29ce484222325;
    for byte in text.as_bytes() {
        hash ^= u64::from(*byte);
        hash = hash.wrapping_mul(0x100000001b3);
    }
    format!("{hash:016x}:{}", text.len())
}

fn region_from_node(node: &Value) -> Option<Value> {
    let region_kind = node.get("regionKind")?.as_str()?;
    let language = node.get("language")?.as_str()?;
    let mut object = JsonMap::new();
    object.insert("kind".to_string(), Value::String(region_kind.to_string()));
    object.insert("language".to_string(), Value::String(language.to_string()));
    object.insert("start".to_string(), node.get("start")?.clone());
    object.insert("end".to_string(), node.get("end")?.clone());
    object.insert(
        "contentStart".to_string(),
        node.get("contentStart")?.clone(),
    );
    object.insert("contentEnd".to_string(), node.get("contentEnd")?.clone());
    if let Some(attributes) = node.get("attributes") {
        object.insert("attributes".to_string(), attributes.clone());
    }
    Some(Value::Object(object))
}

fn range_from_error(text: &str, error: &Value) -> Value {
    let index = TextIndex::new(text);
    let start = error.get("start").and_then(Value::as_u64).unwrap_or(0) as usize;
    let end = error
        .get("end")
        .and_then(Value::as_u64)
        .unwrap_or(start as u64) as usize;
    index.range(start, end)
}

fn token_name(token: &Value) -> Option<&str> {
    token.get("text").and_then(Value::as_str)
}

fn token_range(index: &TextIndex<'_>, token: &Value) -> Value {
    let start = token.get("start").and_then(Value::as_u64).unwrap_or(0) as usize;
    let end = token
        .get("end")
        .and_then(Value::as_u64)
        .unwrap_or(start as u64) as usize;
    index.range(start, end)
}

fn insert_optional_string(object: &mut JsonMap, key: &str, value: &Option<String>) {
    if let Some(value) = value {
        object.insert(key.to_string(), Value::String(value.clone()));
    }
}

fn value_usize(value: &Value, key: &str) -> usize {
    value.get(key).and_then(Value::as_u64).unwrap_or(0) as usize
}

fn offset_from_position(index: &TextIndex<'_>, position: &Value) -> Option<usize> {
    let line = position.get("line")?.as_u64()? as usize;
    let character = position.get("character")?.as_u64()? as usize;
    let start = *index.line_starts.get(line)?;
    let next = index
        .line_starts
        .get(line + 1)
        .copied()
        .unwrap_or(index.len() + 1);
    Some((start + character).min(next.saturating_sub(1)))
}

fn skip_ascii_ws(index: &TextIndex<'_>, mut cursor: usize, end: usize) -> usize {
    while cursor < end && index.char_at(cursor).unwrap_or('\0').is_ascii_whitespace() {
        cursor += 1;
    }
    cursor
}

fn skip_html_ws(index: &TextIndex<'_>, mut cursor: usize, end: usize) -> usize {
    while cursor < end && is_html_ws(index.char_at(cursor).unwrap_or('\0')) {
        cursor += 1;
    }
    cursor
}

fn find_string(index: &TextIndex<'_>, needle: &str, start: usize) -> Option<usize> {
    let haystack = index.slice(start, index.len());
    haystack.find(needle).map(|byte| {
        let prefix = &haystack[..byte];
        start + prefix.encode_utf16().count()
    })
}

fn is_html_ws(ch: char) -> bool {
    matches!(
        ch,
        '\u{0009}'
            | '\u{000a}'
            | '\u{000b}'
            | '\u{000c}'
            | '\u{000d}'
            | '\u{0020}'
            | '\u{00a0}'
            | '\u{1680}'
            | '\u{2028}'
            | '\u{2029}'
            | '\u{202f}'
            | '\u{205f}'
            | '\u{3000}'
            | '\u{feff}'
    ) || ('\u{2000}'..='\u{200a}').contains(&ch)
}

fn is_ascii_alpha(ch: Option<char>) -> bool {
    ch.map(|ch| ch.is_ascii_alphabetic()).unwrap_or(false)
}

fn is_html_tag_part(ch: Option<char>) -> bool {
    ch.map(|ch| ch.is_ascii_alphanumeric() || ch == ':' || ch == '_' || ch == '-')
        .unwrap_or(false)
}

fn is_attr_name_start(ch: Option<char>) -> bool {
    ch.map(|ch| ch.is_ascii_alphabetic() || ch == '_' || ch == ':')
        .unwrap_or(false)
}

fn is_attr_name_part(ch: Option<char>) -> bool {
    ch.map(|ch| ch.is_ascii_alphanumeric() || ch == '-' || ch == '_' || ch == ':' || ch == '.')
        .unwrap_or(false)
}

fn is_identifier_start(ch: char) -> bool {
    ch.is_ascii_alphabetic()
}

fn is_identifier_part(ch: char) -> bool {
    ch.is_ascii_alphanumeric() || ch == '_'
}

fn is_vb_identifier(name: &str) -> bool {
    let mut chars = name.chars();
    let Some(first) = chars.next() else {
        return false;
    };
    is_identifier_start(first) && chars.all(is_identifier_part)
}

fn is_builtin_name(name: &str) -> bool {
    matches!(
        name.to_ascii_lowercase().as_str(),
        "request"
            | "response"
            | "session"
            | "application"
            | "server"
            | "asperror"
            | "true"
            | "false"
            | "nothing"
            | "empty"
            | "null"
            | "me"
    )
}

fn is_implicit_keyword_name(name: &str) -> bool {
    matches!(
        name.to_ascii_lowercase().as_str(),
        "true" | "false" | "nothing" | "empty" | "null" | "me"
    )
}

fn lower_token(token: Option<&VbToken>) -> String {
    token
        .map(|token| token.text.to_ascii_lowercase())
        .unwrap_or_default()
}

fn unquote_vb_string(value: &str) -> String {
    value
        .strip_prefix('"')
        .and_then(|value| value.strip_suffix('"'))
        .map(|value| value.replace("\"\"", "\""))
        .unwrap_or_else(|| value.to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn handle_value(request: Value) -> Value {
        serde_json::from_str(&handle_json(&request.to_string()).expect("handle json"))
            .expect("json result")
    }

    fn read_test_u32(bytes: &[u8], offset: &mut usize) -> u32 {
        let value = u32::from_le_bytes(bytes[*offset..*offset + 4].try_into().expect("u32"));
        *offset += 4;
        value
    }

    #[test]
    fn encodes_vbscript_segments_as_columnar_frame_body() {
        let parsed = parse_asp_document("file:///default.asp", "<%\nDim x\n%>", &json!({}));
        let body =
            encode_document_vbscript_columnar_frame_body(7, &parsed).expect("columnar frame body");
        assert_eq!(body[0], 1);

        let mut offset = 1;
        assert_eq!(read_test_u32(&body, &mut offset), 7);
        assert_eq!(read_test_u32(&body, &mut offset), 1);
        let _node_start = read_test_u32(&body, &mut offset);
        let pool_count = read_test_u32(&body, &mut offset);
        assert!(pool_count > 0);

        offset += pool_count as usize;
        offset += pool_count as usize * 4;
        offset += pool_count as usize * 4;
        let value_count = read_test_u32(&body, &mut offset);
        for _ in 0..value_count {
            let _token_index = read_test_u32(&body, &mut offset);
            let byte_len = read_test_u32(&body, &mut offset);
            offset += byte_len as usize;
        }
        let tree_json_len = read_test_u32(&body, &mut offset) as usize;
        let tree: Value =
            serde_json::from_slice(&body[offset..offset + tree_json_len]).expect("tree json");
        assert!(tree.get("kind").and_then(Value::as_str).is_some());
        assert!(tree.get("children").and_then(Value::as_array).is_some());
        let tokens = tree
            .get("tokens")
            .and_then(Value::as_array)
            .expect("tokens range");
        assert_eq!(tokens.len(), 2);
        assert!(tokens[0].as_u64().unwrap() <= tokens[1].as_u64().unwrap());
    }

    #[test]
    fn parses_server_objects_and_javascript_close_rules() {
        let source = r#"<%@ Language=JScript %>
<object runat="server" id="Catalog" progid="ADODB.Recordset"></object>
<% var marker = '%>'; Response.Write(marker); %>"#;
        let result = handle_value(json!({
            "operation": "parseAspDocument",
            "uri": "file:///default.asp",
            "text": source,
            "settings": {},
        }));
        assert_eq!(result["serverObjects"][0]["id"], "Catalog");
        assert_eq!(result["serverObjects"][0]["progId"], "ADODB.Recordset");
        assert_eq!(result["regions"][2]["language"], "jscript");
        assert_eq!(
            result["regions"][2]["contentEnd"],
            source.find("%>';").unwrap()
        );
    }

    #[test]
    fn closes_vbscript_apostrophe_comments_at_asp_delimiter() {
        let source = "<%\n' comment %>\n<div>html</div>";
        let result = handle_value(json!({
            "operation": "parseAspDocument",
            "uri": "file:///default.asp",
            "text": source,
            "settings": {},
        }));
        assert_eq!(
            result["regions"][0]["contentEnd"],
            source.find("%>").unwrap()
        );
        assert_eq!(result["regions"][1]["language"], "html");
    }

    #[test]
    fn closes_jscript_comments_at_asp_delimiter() {
        let source = r#"<%@ LANGUAGE="JScript" %>
<%
// line comment %>
Response.Write("line")
%>
<%
/* block comment %> */
Response.Write("block")
%>"#;
        let result = handle_value(json!({
            "operation": "parseAspDocument",
            "uri": "file:///default.asp",
            "text": source,
            "settings": {},
        }));
        assert_eq!(
            result["regions"][2]["contentEnd"],
            source.find("%>\nResponse.Write(\"line\")").unwrap()
        );
        assert_eq!(
            result["regions"][4]["contentEnd"],
            source.find("%> */").unwrap()
        );
        assert_eq!(result["regions"][3]["language"], "html");
        assert_eq!(result["regions"][5]["language"], "html");
    }

    #[test]
    fn closes_server_script_at_raw_script_end_tag() {
        let source = r#"<%@ LANGUAGE="JScript" %>
<script runat="server" language="JScript">
function render() {
  var literal = "</script>";
  Response.Write(literal);
}
</script>"#;
        let result = handle_value(json!({
            "operation": "parseAspDocument",
            "uri": "file:///default.asp",
            "text": source,
            "settings": {},
        }));
        let server_script = result["regions"]
            .as_array()
            .unwrap()
            .iter()
            .find(|region| region["kind"] == "server-script")
            .unwrap();
        assert_eq!(server_script["language"], "jscript");
        assert_eq!(
            server_script["end"],
            source.find("</script>").unwrap() + "</script>".len()
        );
        assert!(
            !source[server_script["contentStart"].as_u64().unwrap() as usize
                ..server_script["contentEnd"].as_u64().unwrap() as usize]
                .contains("Response.Write(literal)")
        );
    }

    #[test]
    fn returns_symbol_docs_types_unused_and_external_refs() {
        let source = r#"<%
' Plain docs
' @type title As String
Dim title
Sub Save(usedArg, unusedArg)
  Response.Write usedArg
End Sub
Response.Write MissingValue
%>"#;
        let parsed = handle_value(json!({
            "operation": "parseAspDocument",
            "uri": "file:///default.asp",
            "text": source,
            "settings": {},
        }));
        let analysis = handle_value(json!({
            "operation": "analyzeVbscript",
            "parsed": parsed,
            "context": { "unusedDiagnostics": true },
        }));
        let symbols = analysis["symbols"].as_array().expect("symbols");
        let title = symbols
            .iter()
            .find(|symbol| symbol["name"] == "title")
            .expect("title symbol");
        assert_eq!(title["typeName"], "String");
        assert_eq!(title["documentation"]["format"], "plain");
        assert!(analysis["diagnostics"]
            .as_array()
            .expect("diagnostics")
            .iter()
            .any(
                |diagnostic| diagnostic["source"] == "asp-lsp-vbscript-unused"
                    && diagnostic["tags"][0] == 1
            ));
        let summary = handle_value(json!({
            "operation": "summarizeAspFileAnalysis",
            "parsed": parsed,
        }));
        assert_eq!(
            summary["vbscript"]["externalRefUsages"][0]["name"],
            "MissingValue"
        );
    }
}
