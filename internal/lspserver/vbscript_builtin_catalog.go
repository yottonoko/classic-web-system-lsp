package lspserver

import (
	"context"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

type vbBuiltinMember struct {
	Name       string
	Kind       lsp.CompletionItemKind
	TypeName   string
	Signature  string
	DetailName string
	Docs       localizedBuiltinDocs
	Parameters []localizedBuiltinParameter
}

type localizedBuiltinDocs struct {
	EN string
	JA string
}

type localizedBuiltinParameter struct {
	Name string
	EN   string
	JA   string
}

type vbscriptBuiltinMemberCompletionData struct {
	Kind     string `json:"kind"`
	TypeName string `json:"typeName"`
	Member   string `json:"member"`
}

func vbscriptBuiltinTypeMembers(typeName string) map[string]vbBuiltinMember {
	members := map[string]vbBuiltinMember{}
	add := func(name string, kind lsp.CompletionItemKind, memberType string) {
		members[strings.ToLower(name)] = vbBuiltinMember{Name: name, Kind: kind, TypeName: memberType}
	}
	addMethod := func(name string, returnType string, signature string) {
		members[strings.ToLower(name)] = vbBuiltinMember{Name: name, Kind: lsp.CompletionItemKindMethod, TypeName: returnType, Signature: signature}
	}
	addIndexedProperty := func(name string, memberType string, signature string) {
		members[strings.ToLower(name)] = vbBuiltinMember{Name: name, Kind: lsp.CompletionItemKindProperty, TypeName: memberType, Signature: signature}
	}
	addDoc := func(name string, kind lsp.CompletionItemKind, memberType string, docs localizedBuiltinDocs, params ...localizedBuiltinParameter) {
		members[strings.ToLower(name)] = vbBuiltinMember{Name: name, Kind: kind, TypeName: memberType, Docs: docs, Parameters: params}
	}
	addMethodDoc := func(name string, returnType string, signature string, docs localizedBuiltinDocs, params ...localizedBuiltinParameter) {
		members[strings.ToLower(name)] = vbBuiltinMember{Name: name, Kind: lsp.CompletionItemKindMethod, TypeName: returnType, Signature: signature, Docs: docs, Parameters: params}
	}
	switch strings.ToLower(typeName) {
	case "application":
		add("Contents", lsp.CompletionItemKindProperty, "Variant")
		add("StaticObjects", lsp.CompletionItemKindProperty, "Variant")
		addMethod("Contents.Remove", "Variant", "Application.Contents.Remove(name)")
		addMethod("Contents.RemoveAll", "Variant", "Application.Contents.RemoveAll()")
		addMethod("Lock", "Variant", "Application.Lock")
		addMethod("Unlock", "Variant", "Application.Unlock")
	case "session":
		add("Contents", lsp.CompletionItemKindProperty, "Variant")
		add("StaticObjects", lsp.CompletionItemKindProperty, "Variant")
		add("CodePage", lsp.CompletionItemKindProperty, "Number")
		add("LCID", lsp.CompletionItemKindProperty, "Number")
		add("SessionID", lsp.CompletionItemKindProperty, "String")
		add("Timeout", lsp.CompletionItemKindProperty, "Number")
		addMethod("Abandon", "Variant", "Session.Abandon")
		addMethod("Contents.Remove", "Variant", "Session.Contents.Remove(name)")
		addMethod("Contents.RemoveAll", "Variant", "Session.Contents.RemoveAll()")
	case "response":
		add("Cookies", lsp.CompletionItemKindProperty, "Variant")
		addDoc("Buffer", lsp.CompletionItemKindProperty, "Boolean", localizedBuiltinDocs{
			EN: "Controls whether ASP buffers page output before sending it to the client.\n\n**Values**\n\nSet this before any HTML tag or response output is sent.",
			JA: "ASP の page output を buffer してから client へ送るかを制御します。\n\n**値**\n\nhtml tag より前、または response output より前に設定します。",
		})
		add("CacheControl", lsp.CompletionItemKindProperty, "String")
		add("Charset", lsp.CompletionItemKindProperty, "String")
		add("ContentType", lsp.CompletionItemKindProperty, "String")
		add("Expires", lsp.CompletionItemKindProperty, "Number")
		add("ExpiresAbsolute", lsp.CompletionItemKindProperty, "Date")
		add("IsClientConnected", lsp.CompletionItemKindProperty, "Boolean")
		add("Pics", lsp.CompletionItemKindProperty, "String")
		add("Status", lsp.CompletionItemKindProperty, "String")
		addMethod("AddHeader", "Variant", "Response.AddHeader(name, value)")
		addMethod("AppendToLog", "Variant", "Response.AppendToLog string")
		addMethod("BinaryWrite", "Variant", "Response.BinaryWrite(data)")
		addMethod("Clear", "Variant", "Response.Clear")
		addMethod("End", "Variant", "Response.End")
		addMethod("Flush", "Variant", "Response.Flush")
		addMethod("Redirect", "Variant", "Response.Redirect url")
		addMethod("Write", "Variant", "Response.Write value")
	case "request":
		addIndexedProperty("QueryString", "String", "Request.QueryString(name)")
		addIndexedProperty("Form", "String", "Request.Form(name)")
		addIndexedProperty("Cookies", "Variant", "Request.Cookies(name)")
		addIndexedProperty("ServerVariables", "String", "Request.ServerVariables(name)")
		add("ClientCertificate", lsp.CompletionItemKindProperty, "Variant")
		add("TotalBytes", lsp.CompletionItemKindProperty, "Number")
		addMethod("BinaryRead", "Array", "Request.BinaryRead(count)")
	case "server":
		add("ScriptTimeout", lsp.CompletionItemKindProperty, "Number")
		addMethod("CreateObject", "Object", "Server.CreateObject(progId)")
		addMethodDoc("Execute", "Variant", "Server.Execute(path)", localizedBuiltinDocs{
			EN: "Runs another ASP page and returns to the current page after it finishes.",
			JA: "別の ASP page を実行し、完了後に現在の page へ戻ります。",
		}, localizedBuiltinParameter{
			Name: "path",
			EN:   "Relative or absolute path of the ASP page to execute.",
			JA:   "実行する ASP page の相対 path または絶対 path です。",
		})
		addMethod("GetLastError", "ASPError", "Server.GetLastError()")
		addMethod("HTMLEncode", "String", "Server.HTMLEncode(value)")
		addMethod("MapPath", "String", "Server.MapPath(path)")
		addMethod("Transfer", "Variant", "Server.Transfer(path)")
		addMethod("URLEncode", "String", "Server.URLEncode(value)")
	case "wscript":
		add("Arguments", lsp.CompletionItemKindProperty, "Variant")
		add("FullName", lsp.CompletionItemKindProperty, "String")
		add("Name", lsp.CompletionItemKindProperty, "String")
		add("Path", lsp.CompletionItemKindProperty, "String")
		add("ScriptFullName", lsp.CompletionItemKindProperty, "String")
		add("ScriptName", lsp.CompletionItemKindProperty, "String")
		add("StdErr", lsp.CompletionItemKindProperty, "Object")
		add("StdIn", lsp.CompletionItemKindProperty, "Object")
		add("StdOut", lsp.CompletionItemKindProperty, "Object")
		add("Version", lsp.CompletionItemKindProperty, "String")
		addMethod("ConnectObject", "Variant", "WScript.ConnectObject(object, prefix)")
		addMethod("CreateObject", "Object", "WScript.CreateObject(progId, prefix)")
		addMethod("DisconnectObject", "Variant", "WScript.DisconnectObject object")
		addMethod("Echo", "Variant", "WScript.Echo value")
		addMethod("GetObject", "Object", "WScript.GetObject(pathname, progId, prefix)")
		addMethod("Quit", "Variant", "WScript.Quit errorCode")
		addMethod("Sleep", "Variant", "WScript.Sleep milliseconds")
	case "asperror":
		add("ASPCode", lsp.CompletionItemKindProperty, "String")
		add("ASPDescription", lsp.CompletionItemKindProperty, "String")
		add("Category", lsp.CompletionItemKindProperty, "String")
		add("Column", lsp.CompletionItemKindProperty, "Number")
		add("Description", lsp.CompletionItemKindProperty, "String")
		add("File", lsp.CompletionItemKindProperty, "String")
		add("Line", lsp.CompletionItemKindProperty, "Number")
		add("Number", lsp.CompletionItemKindProperty, "Number")
		add("Source", lsp.CompletionItemKindProperty, "String")
	case "errobject":
		add("Description", lsp.CompletionItemKindProperty, "String")
		add("HelpContext", lsp.CompletionItemKindProperty, "Number")
		add("HelpFile", lsp.CompletionItemKindProperty, "String")
		add("Number", lsp.CompletionItemKindProperty, "Number")
		add("Source", lsp.CompletionItemKindProperty, "String")
		addMethod("Clear", "Variant", "Err.Clear")
		members["raise"] = vbBuiltinMember{
			Name:       "Raise",
			Kind:       lsp.CompletionItemKindMethod,
			TypeName:   "Variant",
			Signature:  "Err.Raise(number, source, description, helpfile, helpcontext)",
			DetailName: "Err",
		}
	case "regexp":
		add("Pattern", lsp.CompletionItemKindProperty, "String")
		add("Global", lsp.CompletionItemKindProperty, "Boolean")
		add("IgnoreCase", lsp.CompletionItemKindProperty, "Boolean")
		add("MultiLine", lsp.CompletionItemKindProperty, "Boolean")
		addMethod("Execute", "Matches", "RegExp.Execute(string)")
		addMethod("Replace", "String", "RegExp.Replace(string, replaceWith)")
		addMethod("Test", "Boolean", "RegExp.Test(string)")
	case "matches":
		add("Count", lsp.CompletionItemKindProperty, "Number")
		addMethod("Item", "Match", "Matches.Item(index)")
	case "match":
		add("FirstIndex", lsp.CompletionItemKindProperty, "Number")
		add("Length", lsp.CompletionItemKindProperty, "Number")
		add("Value", lsp.CompletionItemKindProperty, "String")
		add("SubMatches", lsp.CompletionItemKindProperty, "SubMatches")
	case "submatches":
		add("Count", lsp.CompletionItemKindProperty, "Number")
		addMethod("Item", "String", "SubMatches.Item(index)")
	case "scripting.filesystemobject":
		addMethod("GetFile", "Scripting.File", "FileSystemObject.GetFile(path)")
		addMethod("OpenTextFile", "Scripting.TextStream", "FileSystemObject.OpenTextFile(filename, iomode, create, format)")
	case "scripting.file":
		add("Name", lsp.CompletionItemKindProperty, "String")
		add("Path", lsp.CompletionItemKindProperty, "String")
		addMethod("OpenAsTextStream", "Scripting.TextStream", "File.OpenAsTextStream(iomode, format)")
	case "scripting.dictionary":
		add("Count", lsp.CompletionItemKindProperty, "Number")
		addMethod("Add", "Variant", "Dictionary.Add(key, item)")
		addMethod("Exists", "Boolean", "Dictionary.Exists(key)")
		addMethod("Item", "Variant", "Dictionary.Item(key)")
	case "adodb.stream":
		add("Type", lsp.CompletionItemKindProperty, "Number")
		addMethod("Open", "Variant", "Stream.Open(source, mode, options, userName, password)")
		addMethod("ReadText", "String", "Stream.ReadText(numChars)")
		addMethod("WriteText", "Variant", "Stream.WriteText(data, options)")
	case "adodb.recordset":
		add("EOF", lsp.CompletionItemKindProperty, "Boolean")
		add("Fields", lsp.CompletionItemKindProperty, "ADODB.Fields")
		addMethodDoc("GetRows", "Array", "Recordset.GetRows(rows, start, fields)", localizedBuiltinDocs{
			EN: "Copies records from a Recordset into a two-dimensional array.",
			JA: "Recordset から records を 2 次元 array へ copy します。",
		}, localizedBuiltinParameter{
			Name: "rows",
			EN:   "Number of records to retrieve. When omitted, retrieves the remaining records in the Recordset.",
			JA:   "取得する records 数です。省略すると Recordset の残りを取得します。",
		}, localizedBuiltinParameter{
			Name: "start",
			EN:   "Record number or bookmark where copying starts.",
			JA:   "copy を開始する record number または bookmark です。",
		}, localizedBuiltinParameter{
			Name: "fields",
			EN:   "Field name/number, or an array of field names/numbers to include.",
			JA:   "含める field name/number、または field names/numbers の array です。",
		})
		addMethod("GetString", "String", "Recordset.GetString(format, numRows, columnDelimiter, rowDelimiter, nullExpr)")
		addMethod("MoveNext", "Variant", "Recordset.MoveNext()")
	case "adodb.command":
		add("CommandText", lsp.CompletionItemKindProperty, "String")
		add("Parameters", lsp.CompletionItemKindProperty, "ADODB.Parameters")
		addMethod("CreateParameter", "ADODB.Parameter", "Command.CreateParameter(name, type, direction, size, value)")
		addMethod("Execute", "ADODB.Recordset", "Command.Execute(recordsAffected, parameters, options)")
	case "adodb.parameter":
		add("Name", lsp.CompletionItemKindProperty, "String")
		add("Value", lsp.CompletionItemKindProperty, "Variant")
	}
	return members
}

func (s *Server) vbscriptBuiltinMemberCompletionsContext(ctx context.Context, parsed *core.ParsedDocument, offset int) ([]lsp.CompletionItem, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil || parsed == nil {
		return nil, false
	}
	owner, ok := vbCompletionMemberOwnerBefore(parsed.Text, offset)
	if !ok {
		return nil, false
	}
	typeNames, complete := s.vbscriptKnownTypesForNameAtContext(ctx, parsed, owner, offset)
	if !complete || ctx.Err() != nil {
		return nil, false
	}
	if len(typeNames) == 0 {
		return nil, isVBScriptBuiltinGlobalObjectName(owner)
	}
	membersByName, ok := vbscriptCommonBuiltinMembers(typeNames)
	if !ok || len(membersByName) == 0 {
		return nil, false
	}
	var items []lsp.CompletionItem
	typeName := typeNames[0]
	for _, member := range membersByName {
		item := lsp.CompletionItem{
			Label:  member.Name,
			Kind:   member.Kind,
			Detail: typeName,
			Data: vbscriptBuiltinMemberCompletionData{
				Kind:     "vbscript-builtin-member",
				TypeName: typeName,
				Member:   member.Name,
			},
		}
		item = s.enrichVBScriptBuiltinMemberCompletionItem(typeName, member, item)
		items = append(items, item)
	}
	return items, len(items) > 0
}

func vbscriptCommonBuiltinMembers(typeNames []string) (map[string]vbBuiltinMember, bool) {
	if len(typeNames) == 0 {
		return nil, false
	}
	var common map[string]vbBuiltinMember
	for _, typeName := range typeNames {
		members := vbscriptBuiltinTypeMembers(typeName)
		if len(members) == 0 {
			return nil, false
		}
		if common == nil {
			common = make(map[string]vbBuiltinMember, len(members))
			for name, member := range members {
				common[name] = member
			}
			continue
		}
		for name := range common {
			member, ok := members[strings.ToLower(name)]
			if !ok || !vbscriptTypedMembersCompatible(vbscriptTypedMemberFromBuiltin(common[name]), vbscriptTypedMemberFromBuiltin(member)) {
				delete(common, name)
			}
		}
	}
	return common, true
}

func vbscriptTypedMemberFromBuiltin(member vbBuiltinMember) vbscriptTypedMember {
	kind := "property"
	if member.Kind == lsp.CompletionItemKindMethod {
		kind = "method"
	}
	parameterCount := len(vbscriptBuiltinMemberParameters(member))
	return vbscriptTypedMember{
		Name:                  member.Name,
		TypeName:              member.TypeName,
		Kind:                  kind,
		ParameterCount:        parameterCount,
		ChecksArgumentCount:   member.Signature != "",
		MinimumParameterCount: parameterCount,
		MaximumParameterCount: parameterCount,
		ParameterRangeKnown:   member.Signature != "",
	}
}

func (s *Server) resolveVBScriptBuiltinMemberCompletionItem(item lsp.CompletionItem) lsp.CompletionItem {
	var data vbscriptBuiltinMemberCompletionData
	if remarshal(item.Data, &data) != nil || data.Kind != "vbscript-builtin-member" {
		return item
	}
	member, ok := vbscriptBuiltinTypeMembers(data.TypeName)[strings.ToLower(data.Member)]
	if !ok {
		return item
	}
	return s.enrichVBScriptBuiltinMemberCompletionItem(data.TypeName, member, item)
}

func (s *Server) enrichVBScriptBuiltinMemberCompletionItem(typeName string, member vbBuiltinMember, item lsp.CompletionItem) lsp.CompletionItem {
	if item.Detail == "" {
		item.Detail = typeName
	}
	if doc := s.localizedBuiltinDocs(member.Docs); doc != "" {
		item.Documentation = lsp.MarkupContent{Kind: "markdown", Value: doc}
	}
	return item
}

func (s *Server) vbscriptBuiltinMemberHoverContext(ctx context.Context, parsed *core.ParsedDocument, offset int) *lsp.Hover {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil || parsed == nil {
		return nil
	}
	owner, memberName, ok := vbMemberAtOffset(parsed.Text, offset)
	if !ok {
		return nil
	}
	typeNames, complete := s.vbscriptKnownTypesForNameAtContext(ctx, parsed, owner, offset)
	if !complete || ctx.Err() != nil {
		return nil
	}
	common, ok := vbscriptCommonBuiltinMembers(typeNames)
	if !ok {
		return nil
	}
	member, ok := common[strings.ToLower(memberName)]
	if ok {
		typeName := typeNames[0]
		label := "property " + typeName + "." + member.Name
		if member.Kind == lsp.CompletionItemKindMethod {
			label = memberSignatureLabel(typeName, member)
		} else if member.Signature != "" {
			label = "property " + typeName + "." + member.Name + "(" + strings.Join(builtinParameterNames(vbscriptBuiltinMemberParameters(member)), ", ") + ")"
		}
		if member.TypeName != "" && member.Kind != lsp.CompletionItemKindMethod {
			label += " As " + member.TypeName
		}
		documentation := ""
		if doc := s.localizedBuiltinDocs(member.Docs); doc != "" {
			documentation = doc
		}
		return &lsp.Hover{Contents: lsp.MarkupContent{Kind: "markdown", Value: markdownVBScriptSignature(label, documentation)}}
	}
	return nil
}

func (s *Server) vbscriptBuiltinMemberSignatureHelpContext(ctx context.Context, parsed *core.ParsedDocument, position lsp.Position) *lsp.SignatureHelp {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil || parsed == nil {
		return nil
	}
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	offset := doc.OffsetAt(position)
	open := vbCallOpenParenBefore(parsed.Text, offset)
	if open < 0 {
		return s.vbscriptBuiltinNoParenMemberSignatureHelpContext(ctx, parsed, offset)
	}
	nameStart, nameEnd := vbIdentifierBefore(parsed.Text, open)
	if nameStart < 0 {
		return nil
	}
	ownerStart, ownerEnd := vbMemberOwnerBeforeOffset(parsed.Text, nameStart)
	if ownerStart < 0 {
		return nil
	}
	owner := parsed.Text[ownerStart:ownerEnd]
	memberName := parsed.Text[nameStart:nameEnd]
	typeNames, complete := s.vbscriptKnownTypesForNameAtContext(ctx, parsed, owner, offset)
	if !complete || ctx.Err() != nil {
		return nil
	}
	common, ok := vbscriptCommonBuiltinMembers(typeNames)
	if !ok {
		return nil
	}
	if member, ok := common[strings.ToLower(memberName)]; ok && member.Signature != "" {
		typeName := typeNames[0]
		info := s.vbscriptBuiltinMemberSignatureInformation(typeName, member)
		return &lsp.SignatureHelp{
			Signatures:      []lsp.SignatureInformation{info},
			ActiveSignature: 0,
			ActiveParameter: vbActiveParameter(parsed.Text, open+1, offset),
		}
	}
	return nil
}

func (s *Server) vbscriptBuiltinNoParenMemberSignatureHelpContext(ctx context.Context, parsed *core.ParsedDocument, offset int) *lsp.SignatureHelp {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil || parsed == nil {
		return nil
	}
	if offset < 0 || offset > len(parsed.Text) {
		return nil
	}
	lineStart := offset
	for lineStart > 0 && parsed.Text[lineStart-1] != '\n' && parsed.Text[lineStart-1] != '\r' {
		lineStart--
	}
	statementStart := vbscriptStatementStart(parsed.Text, lineStart, offset)
	cursor := statementStart
	for cursor < offset && isVBWhitespace(parsed.Text[cursor]) {
		cursor++
	}
	if end := readVBIdentifier(parsed.Text, cursor); end > cursor && strings.EqualFold(parsed.Text[cursor:end], "Call") {
		cursor = end
		for cursor < offset && isVBWhitespace(parsed.Text[cursor]) {
			cursor++
		}
	}
	ownerStart := cursor
	ownerEnd := readVBIdentifier(parsed.Text, ownerStart)
	if ownerEnd == ownerStart || ownerEnd >= offset || parsed.Text[ownerEnd] != '.' {
		return nil
	}
	memberStart := ownerEnd + 1
	memberEnd := readVBIdentifier(parsed.Text, memberStart)
	if memberEnd == memberStart {
		return nil
	}
	argsStart := memberEnd
	for argsStart < offset && isVBWhitespace(parsed.Text[argsStart]) {
		argsStart++
	}
	if argsStart >= offset || parsed.Text[argsStart] == '(' {
		return nil
	}
	if !vbscriptNoParenArgumentCursorAtTopLevel(parsed.Text, argsStart, offset) {
		return nil
	}
	owner := parsed.Text[ownerStart:ownerEnd]
	memberName := parsed.Text[memberStart:memberEnd]
	typeNames, complete := s.vbscriptKnownTypesForNameAtContext(ctx, parsed, owner, offset)
	if !complete || ctx.Err() != nil {
		return nil
	}
	common, ok := vbscriptCommonBuiltinMembers(typeNames)
	if !ok {
		return nil
	}
	if member, ok := common[strings.ToLower(memberName)]; ok && member.Signature != "" && member.Kind == lsp.CompletionItemKindMethod {
		typeName := typeNames[0]
		return &lsp.SignatureHelp{
			Signatures:      []lsp.SignatureInformation{s.vbscriptBuiltinMemberSignatureInformation(typeName, member)},
			ActiveSignature: 0,
			ActiveParameter: vbActiveParameter(parsed.Text, argsStart, offset),
		}
	}
	return nil
}

func vbscriptNoParenArgumentCursorAtTopLevel(text string, start, offset int) bool {
	depth := 0
	inString := false
	for index := start; index < offset; index++ {
		switch text[index] {
		case '"':
			if inString && index+1 < offset && text[index+1] == '"' {
				index++
				continue
			}
			inString = !inString
		case '(':
			if !inString {
				depth++
			}
		case ')':
			if !inString && depth > 0 {
				depth--
			}
		}
	}
	return depth == 0
}

func (s *Server) vbscriptBuiltinMemberSignatureInformation(typeName string, member vbBuiltinMember) lsp.SignatureInformation {
	params := vbscriptBuiltinMemberParameters(member)
	detailName := typeName
	if member.DetailName != "" {
		detailName = member.DetailName
	}
	return lsp.SignatureInformation{
		Label:         detailName + "." + member.Name + "(" + strings.Join(builtinParameterNames(params), ", ") + ")",
		Documentation: s.localizedBuiltinDocs(member.Docs),
		Parameters:    s.localizedBuiltinParameterInformation(params),
	}
}

func vbscriptBuiltinMemberParameters(member vbBuiltinMember) []localizedBuiltinParameter {
	if len(member.Parameters) > 0 {
		return member.Parameters
	}
	signature := strings.TrimSpace(member.Signature)
	if signature == "" {
		return nil
	}
	var raw string
	if open := strings.IndexByte(signature, '('); open >= 0 {
		if close := strings.LastIndexByte(signature, ')'); close > open {
			raw = signature[open+1 : close]
		}
	} else if space := strings.IndexAny(signature, " \t"); space >= 0 {
		raw = signature[space+1:]
	}
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	params := make([]localizedBuiltinParameter, 0, len(parts))
	for _, part := range parts {
		if name := strings.TrimSpace(part); name != "" {
			params = append(params, localizedBuiltinParameter{Name: name})
		}
	}
	return params
}

func builtinParameterNames(params []localizedBuiltinParameter) []string {
	names := make([]string, 0, len(params))
	for _, param := range params {
		names = append(names, param.Name)
	}
	return names
}

func (s *Server) localizedBuiltinDocs(docs localizedBuiltinDocs) string {
	if s.isJapanese() && docs.JA != "" {
		return docs.JA
	}
	return docs.EN
}

func (s *Server) localizedBuiltinParameterInformation(params []localizedBuiltinParameter) []lsp.ParameterInformation {
	if len(params) == 0 {
		return nil
	}
	items := make([]lsp.ParameterInformation, 0, len(params))
	for _, param := range params {
		doc := param.EN
		if s.isJapanese() && param.JA != "" {
			doc = param.JA
		}
		items = append(items, lsp.ParameterInformation{Label: param.Name, Documentation: doc})
	}
	return items
}

func memberSignatureLabel(typeName string, member vbBuiltinMember) string {
	signature := member.Signature
	if signature == "" {
		signature = typeName + "." + member.Name + "()"
	}
	prefix := "Sub "
	if member.TypeName != "" && !strings.EqualFold(member.TypeName, "Variant") {
		prefix = "Function "
	}
	label := prefix + signature
	if member.TypeName != "" && !strings.EqualFold(member.TypeName, "Variant") {
		label += " As " + member.TypeName
	}
	return label
}

func vbCallOpenParenBefore(text string, offset int) int {
	open, _ := vbCallOpenParenBeforeResult(text, offset)
	return open
}

func vbCallOpenParenBeforeResult(text string, offset int) (int, bool) {
	if offset < 0 {
		return -1, false
	}
	if offset > len(text) {
		offset = len(text)
	}
	lineStart := offset
	for lineStart > 0 && text[lineStart-1] != '\n' && text[lineStart-1] != '\r' {
		lineStart--
	}
	stack := make([]int, 0, 4)
	inString := false
	statementContentSeen := false
	for i := lineStart; i < offset; i++ {
		if !inString && !statementContentSeen {
			if text[i] == ' ' || text[i] == '\t' {
				continue
			}
			if i+3 <= offset && strings.EqualFold(text[i:i+3], "Rem") && (i+3 == offset || isVBWhitespace(text[i+3])) {
				return -1, true
			}
			statementContentSeen = true
		}
		switch text[i] {
		case '"':
			if inString && i+1 < offset && text[i+1] == '"' {
				i++
				continue
			}
			inString = !inString
		case '\'':
			if !inString {
				return -1, true
			}
		case ':':
			if !inString {
				stack = stack[:0]
				statementContentSeen = false
			}
		case '(':
			if !inString {
				stack = append(stack, i)
			}
		case ')':
			if !inString && len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if len(stack) == 0 {
		return -1, false
	}
	return stack[len(stack)-1], false
}

func vbIdentifierBefore(text string, offset int) (int, int) {
	i := offset - 1
	for i >= 0 && (text[i] == ' ' || text[i] == '\t') {
		i--
	}
	end := i + 1
	for i >= 0 && isVBIdentifierByte(text[i]) {
		i--
	}
	start := i + 1
	if start == end {
		return -1, -1
	}
	return start, end
}

func vbMemberOwnerBeforeOffset(text string, offset int) (int, int) {
	i := offset - 1
	for i >= 0 && (text[i] == ' ' || text[i] == '\t') {
		i--
	}
	if i < 0 || text[i] != '.' {
		return -1, -1
	}
	end := i
	i--
	for i >= 0 && isVBIdentifierByte(text[i]) {
		i--
	}
	start := i + 1
	if start == end {
		return -1, -1
	}
	return start, end
}

func vbActiveParameter(text string, start, offset int) int {
	if offset < start {
		return 0
	}
	if offset > len(text) {
		offset = len(text)
	}
	active := 0
	depth := 0
	inString := false
	for i := start; i < offset; i++ {
		switch text[i] {
		case '"':
			inString = !inString
		case '(':
			if !inString {
				depth++
			}
		case ')':
			if !inString && depth > 0 {
				depth--
			}
		case ',':
			if !inString && depth == 0 {
				active++
			}
		}
	}
	return active
}

// vbscriptKnownTypesForNameAtContext reads only source-ordered type state for
// the request offset; an empty result is authoritative.
func (s *Server) vbscriptKnownTypesForNameAtContext(ctx context.Context, parsed *core.ParsedDocument, name string, offset int) ([]string, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if parsed == nil || strings.TrimSpace(name) == "" || ctx.Err() != nil {
		return nil, false
	}
	info, complete := s.vbscriptTypeInfoAtOffsetContext(ctx, parsed, offset)
	if !complete || ctx.Err() != nil {
		return nil, false
	}
	scope := vbscriptScopeAtOffset(parsed, offset)
	typeName := info.scopedVariableTypeNames[vbscriptTypeScopeKey(parsed, scope, name)]
	bound := vbscriptNameBoundInScopeAtOffset(parsed, name, scope, offset)
	if typeName == "" && (scope == "" || !bound) {
		typeName = info.variableTypes[strings.ToLower(name)]
	}
	if typeName == "" && !bound {
		if builtinType, ok := vbscriptBuiltinGlobalObjectType(parsed, name); ok {
			typeName = builtinType
		}
	}
	return vbscriptConcreteTypeNames(typeName), true
}

func vbscriptBuiltinGlobalObjectType(parsed *core.ParsedDocument, name string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "err" {
		return "ErrObject", true
	}
	if isStandaloneVBScriptDocument(parsed) {
		if lower == "wscript" {
			return "WScript", true
		}
		return "", false
	}
	switch lower {
	case "application":
		return "Application", true
	case "session":
		return "Session", true
	case "response":
		return "Response", true
	case "request":
		return "Request", true
	case "server":
		return "Server", true
	default:
		return "", false
	}
}

func isVBScriptBuiltinGlobalObjectName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "application", "session", "response", "request", "server", "wscript", "err":
		return true
	default:
		return false
	}
}

func vbMemberAtOffset(text string, offset int) (string, string, bool) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(text) {
		offset = len(text)
	}
	start := offset
	for start > 0 && isVBIdentifierByte(text[start-1]) {
		start--
	}
	end := offset
	for end < len(text) && isVBIdentifierByte(text[end]) {
		end++
	}
	if start == end || start == 0 || text[start-1] != '.' {
		return "", "", false
	}
	ownerEnd := start - 1
	ownerStart := ownerEnd
	for ownerStart > 0 && isVBIdentifierByte(text[ownerStart-1]) {
		ownerStart--
	}
	if ownerStart == ownerEnd {
		return "", "", false
	}
	return text[ownerStart:ownerEnd], text[start:end], true
}

func vbscriptBuiltinValueType(value string) string {
	lower := strings.ToLower(strings.TrimSpace(value))
	catalogType := vbscriptBuiltinCatalogValueType(value)
	switch {
	case strings.HasPrefix(lower, "server.createobject(\"vbscript.regexp\""), strings.HasPrefix(lower, "wscript.createobject(\"vbscript.regexp\""):
		return "RegExp"
	case strings.HasPrefix(lower, "server.createobject("), strings.HasPrefix(lower, "wscript.createobject("):
		if typeName := quotedVBArgument(value); typeName != "" {
			return typeName
		}
		return "Object"
	case strings.HasPrefix(lower, "server.getlasterror("):
		return "ASPError"
	case strings.HasPrefix(lower, "new regexp"):
		return "RegExp"
	case strings.HasPrefix(lower, "createobject(\"vbscript.regexp\""):
		return "RegExp"
	case catalogType != "":
		return catalogType
	case strings.Contains(lower, ".getfile("):
		return "Scripting.File"
	case strings.Contains(lower, ".getrows("):
		return "Array"
	case strings.Contains(lower, ".getstring("):
		return "String"
	case strings.Contains(lower, ".createparameter("):
		return "ADODB.Parameter"
	case strings.Contains(lower, ".execute("):
		return "Matches"
	case strings.Contains(lower, ".replace("):
		return "String"
	case strings.Contains(lower, ".test("):
		return "Boolean"
	case strings.Contains(lower, ".submatches"):
		return "SubMatches"
	case strings.Contains(lower, ".item("):
		return vbscriptBuiltinItemType(value)
	default:
		return ""
	}
}

func vbscriptBuiltinCatalogValueType(value string) string {
	value = strings.TrimSpace(value)
	ownerEnd := readVBIdentifier(value, 0)
	if ownerEnd == 0 {
		return ""
	}
	owner := value[:ownerEnd]
	typeName, ok := vbscriptBuiltinGlobalObjectTypeForName(owner)
	if !ok {
		return ""
	}
	cursor := ownerEnd
	for cursor < len(value) && isVBWhitespace(value[cursor]) {
		cursor++
	}
	if cursor >= len(value) {
		return typeName
	}
	if value[cursor] != '.' {
		return ""
	}
	cursor++
	for cursor < len(value) && isVBWhitespace(value[cursor]) {
		cursor++
	}
	memberEnd := readVBIdentifier(value, cursor)
	if memberEnd == cursor {
		return ""
	}
	member, ok := vbscriptBuiltinTypeMembers(typeName)[strings.ToLower(value[cursor:memberEnd])]
	if !ok {
		return ""
	}
	if member.TypeName == "" {
		return "Variant"
	}
	return member.TypeName
}

func vbscriptBuiltinGlobalObjectTypeForName(name string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "application":
		return "Application", true
	case "session":
		return "Session", true
	case "response":
		return "Response", true
	case "request":
		return "Request", true
	case "server":
		return "Server", true
	case "wscript":
		return "WScript", true
	default:
		return "", false
	}
}

func vbscriptBuiltinItemType(value string) string {
	lower := strings.ToLower(strings.TrimSpace(value))
	ownerEnd := strings.Index(lower, ".item(")
	if ownerEnd <= 0 {
		return ""
	}
	owner := strings.TrimSpace(value[:ownerEnd])
	ownerTypes := vbscriptKnownTypesForTextName(value, owner)
	switch {
	case len(ownerTypes) > 0 && strings.EqualFold(ownerTypes[0], "Matches"):
		return "Match"
	case len(ownerTypes) > 0 && strings.EqualFold(ownerTypes[0], "SubMatches"):
		return "String"
	default:
		if strings.Contains(lower, "matches.item(") {
			return "Match"
		}
		return ""
	}
}

func vbscriptKnownTypesForTextName(text string, name string) []string {
	if name == "" {
		return nil
	}
	var types []string
	for _, line := range strings.Split(text, "\n") {
		for _, statement := range splitVBStatements(strings.TrimRight(line, "\r")) {
			if typeName, ok := assignedVBValueTypeFromStatement(statement, strings.ToLower(name)); ok {
				types = append(types, typeName)
			}
		}
	}
	return types
}
