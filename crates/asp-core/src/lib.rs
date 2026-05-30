use serde_json::{json, Map, Value};
use std::collections::HashSet;

type JsonMap = Map<String, Value>;

pub fn handle_json(input: &str) -> Result<String, String> {
    let request: Value = serde_json::from_str(input).map_err(|error| error.to_string())?;
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
            parse_asp_document(uri, text, settings)
        }
        "collectVbscriptSymbols" => {
            let parsed = request
                .get("parsed")
                .ok_or_else(|| "parsed is required".to_string())?;
            let symbols = collect_symbols_from_parsed(parsed);
            Value::Array(symbols)
        }
        "summarizeAspFileAnalysis" => {
            let parsed = request
                .get("parsed")
                .ok_or_else(|| "parsed is required".to_string())?;
            summarize_asp_file(parsed)
        }
        "analyzeVbscript" => {
            let parsed = request
                .get("parsed")
                .ok_or_else(|| "parsed is required".to_string())?;
            let symbols = collect_symbols_from_parsed(parsed);
            let diagnostics = diagnose_vbscript(parsed, &symbols);
            json!({ "diagnostics": diagnostics, "symbols": symbols })
        }
        _ => return Err(format!("unknown operation: {operation}")),
    };
    serde_json::to_string(&result).map_err(|error| error.to_string())
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
    let default_language = default_language_from_directives(&directives, settings);
    json!({
        "uri": uri,
        "text": text,
        "cst": cst,
        "regions": regions,
        "directives": directives,
        "includes": includes,
        "defaultLanguage": default_language,
        "diagnostics": diagnostics,
    })
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
        diagnostics: Vec::new(),
    };
    let mut cursor = 0usize;
    while cursor < index.len() {
        if index.starts_with(cursor, "<%") {
            let region = parse_asp_region_at(index, cursor, index.len(), settings, &mut scan);
            cursor = region.end;
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
        let Some(tag) = read_html_tag(index, cursor) else {
            cursor += 1;
            continue;
        };
        if !tag.closing {
            scan.tag_regions
                .extend(style_attribute_regions_from_tag(&tag));
            let nested = scan_asp_regions_in_range(
                index,
                tag.attributes_start,
                tag.attributes_end,
                settings,
                &mut scan,
            );
            scan.inline_regions.extend(nested);
        }
        if (tag.name == "script" || tag.name == "style") && !tag.closing && !tag.self_closing {
            if let Some((close_start, close_end)) = find_element_close(index, &tag.name, tag.end) {
                scan.tag_regions
                    .push(element_region_from_tag(&tag, close_start, close_end));
                let nested =
                    scan_asp_regions_in_range(index, tag.end, close_start, settings, &mut scan);
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
        let region = parse_asp_region_at(index, next, end, settings, scan);
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
) -> Region {
    let close = find_asp_close(index, start + 2, max_end);
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

fn find_asp_close(index: &TextIndex<'_>, offset: usize, max_end: usize) -> Option<usize> {
    let mut vb_quote: Option<char> = None;
    let mut vb_line_comment = false;
    let mut vb_block_comment = false;
    let mut js_quote: Option<char> = None;
    let mut js_line_comment = false;
    let mut js_block_comment = false;
    let mut cursor = offset;
    while cursor < max_end {
        let char = index.char_at(cursor).unwrap_or('\0');
        let next = index.char_at(cursor + 1).unwrap_or('\0');
        if vb_line_comment {
            if char == '\r' || char == '\n' {
                vb_line_comment = false;
            }
        } else if vb_block_comment {
            if char == '*' && next == '/' {
                vb_block_comment = false;
                cursor += 1;
            }
        } else if let Some(quote) = vb_quote {
            if char == quote {
                if quote == '"' && next == '"' {
                    cursor += 1;
                } else {
                    vb_quote = None;
                }
            }
        } else if char == '%' && next == '>' {
            return Some(cursor);
        } else if char == '\'' {
            vb_line_comment = true;
        } else if char == '/' && next == '*' {
            vb_block_comment = true;
            cursor += 1;
        } else if char == '/' && next == '/' {
            vb_line_comment = true;
            cursor += 1;
        } else if char == '"' {
            vb_quote = Some(char);
        }

        if js_line_comment {
            if char == '\r' || char == '\n' {
                js_line_comment = false;
            }
        } else if js_block_comment {
            if char == '*' && next == '/' {
                js_block_comment = false;
                cursor += 1;
            }
        } else if let Some(quote) = js_quote {
            // TS 版 (asp-scanner.ts find_asp_close) と挙動を一致させる。
            if quote == '\'' && (char == '\r' || char == '\n') {
                js_quote = None;
            } else if char == '\\' {
                cursor += 1;
            } else if char == quote {
                if quote == '"' && next == '"' {
                    cursor += 1;
                } else {
                    js_quote = None;
                }
            }
        } else if char == '%' && next == '>' {
            return Some(cursor);
        } else if char == '"' || char == '\'' || char == '`' {
            js_quote = Some(char);
        } else if char == '/' && next == '/' {
            js_line_comment = true;
            cursor += 1;
        } else if char == '/' && next == '*' {
            js_block_comment = true;
            cursor += 1;
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

fn read_html_tag(index: &TextIndex<'_>, start: usize) -> Option<HtmlTag> {
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
    let tag_end = find_tag_end(index, cursor)?;
    let attributes_start = cursor;
    let attributes_end = tag_end;
    let attribute_spans = parse_attribute_spans(index, attributes_start, attributes_end);
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

fn find_tag_end(index: &TextIndex<'_>, offset: usize) -> Option<usize> {
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
            let close = find_asp_close(index, cursor + 2, index.len())?;
            cursor = close + 1;
        } else if ch == '>' {
            return Some(cursor);
        }
        cursor += 1;
    }
    None
}

fn parse_attribute_spans(index: &TextIndex<'_>, start: usize, end: usize) -> Vec<AttributeSpan> {
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
            cursor = find_asp_close(index, cursor + 2, end)
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
    if tag_name == "script" {
        return find_script_close(index, tag_name, offset);
    }
    if tag_name == "style" {
        return find_style_close(index, tag_name, offset);
    }
    let close = format!("</{tag_name}");
    let mut cursor = offset;
    while cursor < index.len() {
        if index
            .slice(cursor, index.len())
            .to_ascii_lowercase()
            .starts_with(&close)
        {
            let end = find_tag_end(index, cursor + 2)?;
            return Some((cursor, end + 1));
        }
        cursor += 1;
    }
    None
}

fn find_script_close(
    index: &TextIndex<'_>,
    tag_name: &str,
    offset: usize,
) -> Option<(usize, usize)> {
    let close = format!("</{tag_name}");
    let mut quote: Option<char> = None;
    let mut line_comment = false;
    let mut block_comment = false;
    let mut cursor = offset;
    while cursor < index.len() {
        let char = index.char_at(cursor).unwrap_or('\0');
        let next = index.char_at(cursor + 1).unwrap_or('\0');
        if line_comment {
            if char == '\r' || char == '\n' {
                line_comment = false;
            }
            cursor += 1;
            continue;
        }
        if block_comment {
            if char == '*' && next == '/' {
                block_comment = false;
                cursor += 2;
                continue;
            }
            cursor += 1;
            continue;
        }
        if let Some(current_quote) = quote {
            if char == '\\' && current_quote != '`' {
                cursor += 2;
                continue;
            }
            if char == current_quote {
                quote = None;
            }
            cursor += 1;
            continue;
        }
        if index
            .slice(cursor, index.len())
            .to_ascii_lowercase()
            .starts_with(&close)
        {
            let end = find_tag_end(index, cursor + 2)?;
            return Some((cursor, end + 1));
        }
        if char == '"' || char == '\'' || char == '`' {
            quote = Some(char);
        } else if char == '/' && next == '/' {
            line_comment = true;
            cursor += 1;
        } else if char == '/' && next == '*' {
            block_comment = true;
            cursor += 1;
        }
        cursor += 1;
    }
    None
}

fn find_style_close(
    index: &TextIndex<'_>,
    tag_name: &str,
    offset: usize,
) -> Option<(usize, usize)> {
    let close = format!("</{tag_name}");
    let mut quote: Option<char> = None;
    let mut block_comment = false;
    let mut cursor = offset;
    while cursor < index.len() {
        let char = index.char_at(cursor).unwrap_or('\0');
        let next = index.char_at(cursor + 1).unwrap_or('\0');
        if block_comment {
            if char == '*' && next == '/' {
                block_comment = false;
                cursor += 2;
                continue;
            }
            cursor += 1;
            continue;
        }
        if let Some(current_quote) = quote {
            if char == current_quote {
                quote = None;
            }
            cursor += 1;
            continue;
        }
        if index
            .slice(cursor, index.len())
            .to_ascii_lowercase()
            .starts_with(&close)
        {
            let end = find_tag_end(index, cursor + 2)?;
            return Some((cursor, end + 1));
        }
        if char == '"' || char == '\'' {
            quote = Some(char);
        } else if char == '/' && next == '*' {
            block_comment = true;
            cursor += 1;
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
    let spans = parse_attribute_spans(&index, 0, index.len());
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
    let tokens = tokenize_vbscript(text, base_offset);
    let significant = tokens
        .iter()
        .filter(|token| token.kind != "whitespace" && token.kind != "comment")
        .cloned()
        .collect::<Vec<_>>();
    let mut document = VbNode {
        kind: "Document".to_string(),
        start: base_offset,
        end: base_offset + TextIndex::new(text).len(),
        content_start: Some(base_offset),
        content_end: Some(base_offset + TextIndex::new(text).len()),
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
    close_unclosed(
        &mut document,
        &mut stack,
        base_offset + TextIndex::new(text).len(),
    );
    vb_node_to_value(&document)
}

fn tokenize_vbscript(text: &str, base_offset: usize) -> Vec<VbToken> {
    let index = TextIndex::new(text);
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
            tokens.push(vb_token(
                "newline",
                &index,
                start,
                cursor,
                base_offset,
                None,
            ));
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
                &index,
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
            tokens.push(vb_token(
                "comment",
                &index,
                start,
                cursor,
                base_offset,
                None,
            ));
            continue;
        }
        if is_rem_comment_start(&index, cursor) {
            cursor += 3;
            while cursor < index.len() {
                let c = index.char_at(cursor).unwrap_or('\0');
                if c == '\r' || c == '\n' {
                    break;
                }
                cursor += 1;
            }
            tokens.push(vb_token(
                "comment",
                &index,
                start,
                cursor,
                base_offset,
                None,
            ));
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
                &index,
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
            tokens.push(vb_token(kind, &index, start, cursor, base_offset, None));
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
            tokens.push(vb_token("number", &index, start, cursor, base_offset, None));
            continue;
        }
        cursor += 1;
        tokens.push(vb_token("symbol", &index, start, cursor, base_offset, None));
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

fn vb_keywords() -> HashSet<&'static str> {
    [
        "and", "as", "byref", "byval", "call", "case", "class", "const", "dim", "do", "each",
        "else", "elseif", "empty", "end", "exit", "explicit", "false", "for", "function", "get",
        "if", "in", "is", "let", "loop", "me", "mod", "new", "next", "not", "nothing", "null",
        "option", "or", "preserve", "private", "property", "public", "redim", "rem", "select",
        "set", "step", "sub", "then", "to", "true", "until", "wend", "while", "with",
    ]
    .into_iter()
    .collect()
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
            if let Some(array) = read_array_declaration(tokens, index, end_index, declaration_kind) {
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

fn collect_symbols_from_parsed(parsed: &Value) -> Vec<Value> {
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
            collect_symbols_from_node(&cst, uri, &text_index, None, None, &mut symbols);
        }
    }
    symbols
}

fn collect_symbols_from_node(
    node: &Value,
    uri: &str,
    text_index: &TextIndex<'_>,
    member_of: Option<String>,
    scope_name: Option<String>,
    symbols: &mut Vec<Value>,
) {
    let kind = node.get("kind").and_then(Value::as_str).unwrap_or_default();
    let mut current_member = member_of.clone();
    let mut current_scope = scope_name.clone();
    if kind == "Class" {
        if let Some(name) = node.get("nameToken").and_then(token_name) {
            let range = token_range(text_index, node.get("nameToken").unwrap());
            symbols.push(json!({
                "name": name,
                "kind": "class",
                "range": range,
                "sourceUri": uri,
            }));
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
            }
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
            symbols.push(Value::Object(symbol));
            current_scope = Some(name.to_string());
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
                                "memberOf": member_of,
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
                    symbols.push(json!({
                        "name": name,
                        "kind": if kind == "ConstantDeclaration" { "constant" } else { "variable" },
                        "range": token_range(text_index, token),
                        "sourceUri": uri,
                        "memberOf": member_of,
                        "scopeName": scope_name,
                        "visibility": node.get("visibility").and_then(Value::as_str),
                    }));
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
                current_member.clone(),
                current_scope.clone(),
                symbols,
            );
        }
    }
}

fn diagnose_vbscript(parsed: &Value, symbols: &[Value]) -> Vec<Value> {
    let mut diagnostics = Vec::new();
    let text = parsed
        .get("text")
        .and_then(Value::as_str)
        .unwrap_or_default();
    let text_index = TextIndex::new(text);
    if !text.to_ascii_lowercase().contains("option explicit") {
        return diagnostics;
    }
    let declared = symbols
        .iter()
        .filter_map(|symbol| symbol.get("name").and_then(Value::as_str))
        .map(|name| name.to_ascii_lowercase())
        .collect::<HashSet<_>>();
    let builtins = [
        "request",
        "response",
        "session",
        "application",
        "server",
        "asperror",
    ];
    let builtin_set = builtins.into_iter().collect::<HashSet<_>>();
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
            let tokens = tokenize_vbscript(text_index.slice(start, end), start);
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
                if declared.contains(&lower) || builtin_set.contains(lower.as_str()) {
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
    diagnostics
}

fn previous_significant_token(tokens: &[VbToken], index: usize) -> Option<&VbToken> {
    tokens[..index].iter().rev().find(|token| {
        token.kind != "whitespace" && token.kind != "comment" && token.kind != "newline"
    })
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
    let symbols = collect_symbols_from_parsed(parsed);
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
    let vbscript = if regions
        .iter()
        .any(|region| region.get("language").and_then(Value::as_str) == Some("vbscript"))
    {
        Some(json!({
            "fingerprint": fingerprint(text),
            "localSymbols": symbols,
            "publicSymbols": public_symbols,
            "exports": exports,
            "externalRefs": [],
            "externalRefUsages": [],
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
