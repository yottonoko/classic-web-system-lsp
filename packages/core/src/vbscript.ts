import {
  CompletionItemKind,
  DiagnosticSeverity,
  DocumentHighlightKind,
  InlayHintKind,
  InsertTextFormat,
  SymbolKind,
} from "vscode-languageserver-types";
import type {
  CallHierarchyIncomingCall,
  CallHierarchyItem,
  CallHierarchyOutgoingCall,
  CompletionItem,
  Diagnostic,
  DocumentHighlight,
  DocumentSymbol,
  InlayHint,
  Position,
  Range,
  SelectionRange,
  SignatureHelp,
  TextEdit,
} from "vscode-languageserver-types";
import { offsetAt, positionAt, rangeFromOffsets } from "./position";
import { createLocalizer } from "./localize";
import type {
  AspCstNode,
  AspLocale,
  AspParsedDocument,
  AspRegion,
  AspVbscriptComType,
  AspVbscriptIdentifierCase,
  AspVbscriptIdentifierKind,
  VbArrayDeclaration,
  VbCstNode,
  VbParameterMetadata,
  VbParameterMode,
  VbToken,
} from "./types";

export type VbSymbolKind =
  | "variable"
  | "parameter"
  | "constant"
  | "function"
  | "sub"
  | "class"
  | "method"
  | "field"
  | "property";

export interface VbSymbol {
  name: string;
  kind: VbSymbolKind;
  range: Range;
  sourceUri: string;
  containerName?: string;
  memberOf?: string;
  scopeName?: string;
  scopeRange?: Range;
  typeName?: string;
  type?: VbTypeRef;
  explicitType?: boolean;
  parameters?: string[];
  parameterDetails?: VbParameterInfo[];
  visibility?: "public" | "private";
  procedureKind?: "sub" | "function";
  propertyAccessor?: "get" | "let" | "set";
  parameterMode?: VbParameterMode;
  optional?: boolean;
  implicit?: boolean;
  array?: {
    kind: "fixed" | "dynamic";
    dimensions: string[];
  };
  documentation?: VbDocumentation;
}

export interface VbSemanticToken {
  range: Range;
  tokenType: "variable" | "parameter" | "function" | "class" | "method" | "property" | "operator";
  tokenModifiers?: Array<"public" | "private" | "readonly" | "library" | "byref" | "byval">;
}

export interface VbReference {
  uri: string;
  range: Range;
}

export interface VbReferenceOptions {
  includeDeclaration?: boolean;
  includeFunctionReturnAssignments?: boolean;
}

export interface VbProjectContext {
  symbols?: VbSymbol[];
  documents?: AspParsedDocument[];
  typeChecking?: "basic" | "strict";
  identifierCase?: AspVbscriptIdentifierCase;
  identifierCaseByKind?: Partial<Record<AspVbscriptIdentifierKind, AspVbscriptIdentifierCase>>;
  comTypes?: Record<string, AspVbscriptComType>;
  typeEnvironment?: VbTypeEnvironment;
  unusedDiagnostics?: boolean;
  syntaxSnippets?: boolean;
  locale?: AspLocale;
}

export interface VbInlayHintOptions {
  variableTypes?: boolean;
  parameterNames?: boolean;
  functionReturnTypes?: boolean;
  implicitByRef?: boolean;
}

export interface VbCallHierarchyData {
  uri: string;
  name: string;
  kind: VbSymbolKind;
  memberOf?: string;
  rootUri?: string;
  line: number;
  character: number;
}

export interface VbTypeRef {
  name: string;
  object?: boolean;
  unionTypes?: VbTypeRef[];
}

export interface VbParameterInfo {
  name: string;
  mode: VbParameterMode;
  optional?: boolean;
}

export interface VbSignatureParameter {
  name: string;
  type?: VbTypeRef;
  mode?: VbParameterMode;
  optional?: boolean;
  documentation?: string;
}

export interface VbSignature {
  parameters: VbSignatureParameter[];
  returnType?: VbTypeRef;
  documentation?: string;
}

type VbCallSyntaxDiagnosticCode =
  | "callStatementRequiresParentheses"
  | "expressionCallRequiresParentheses"
  | "statementCallDisallowsParenthesizedArguments";

export interface VbMember {
  name: string;
  kind: "field" | "property" | "method" | "event";
  type?: VbTypeRef;
  signature?: VbSignature;
  documentation?: string;
}

export interface VbType {
  name: string;
  kind: "intrinsic" | "classicAsp" | "class" | "com";
  members: VbMember[];
}

export interface VbTypeEnvironment {
  types: VbType[];
  symbols: VbSymbol[];
}

export type VbTypeDiagnostic = Diagnostic;

export interface VbDocumentation {
  summary?: string;
  remarks?: string;
  params: Record<string, string>;
  returns?: string;
  value?: string;
  exceptions: Array<{ cref?: string; text: string }>;
  see: Array<{ cref?: string; href?: string; langword?: string; text?: string }>;
  seealso: Array<{ cref?: string; href?: string; langword?: string; text?: string }>;
  example?: string;
  code?: string;
  ambiguousTarget?: boolean;
}

export interface VbDocumentationQuickAction {
  symbol: VbSymbol;
  edits: TextEdit[];
}

interface VbDocElement {
  name: string;
  attributes: Record<string, string>;
  children: Array<VbDocElement | string>;
  selfClosing?: boolean;
}

interface VbDocTagToken {
  kind: "start" | "end";
  name: string;
  selfClosing: boolean;
}

type VbDocXmlToken =
  | {
      kind: "text";
      start: number;
      end: number;
      text: string;
    }
  | {
      kind: "start";
      start: number;
      end: number;
      name: string;
      attributes: Record<string, string>;
      selfClosing: boolean;
    }
  | {
      kind: "end";
      start: number;
      end: number;
      name: string;
    };

const vbDocCommentTags = [
  "summary",
  "remarks",
  "param",
  "returns",
  "value",
  "exception",
  "see",
  "seealso",
  "example",
  "code",
  "c",
  "list",
  "para",
] as const;

const vbDocCommentAttributeCompletions: Record<string, string[]> = {
  param: ["name"],
  exception: ["cref"],
  see: ["cref", "href", "langword"],
  seealso: ["cref", "href", "langword"],
  list: ["type"],
};

function builtinCompletions(locale: AspLocale | undefined): CompletionItem[] {
  const localizer = createLocalizer(locale);
  return [
    withBuiltinCompletionLabel(
      {
        label: "Request",
        kind: CompletionItemKind.Variable,
        detail: localizer.t("vb.builtin.request.detail"),
        documentation: localizer.t("vb.builtin.request.documentation"),
      },
      locale,
    ),
    withBuiltinCompletionLabel(
      {
        label: "Response",
        kind: CompletionItemKind.Variable,
        detail: localizer.t("vb.builtin.response.detail"),
      },
      locale,
    ),
    withBuiltinCompletionLabel(
      {
        label: "Session",
        kind: CompletionItemKind.Variable,
        detail: localizer.t("vb.builtin.session.detail"),
      },
      locale,
    ),
    withBuiltinCompletionLabel(
      {
        label: "Application",
        kind: CompletionItemKind.Variable,
        detail: localizer.t("vb.builtin.application.detail"),
      },
      locale,
    ),
    withBuiltinCompletionLabel(
      {
        label: "Server",
        kind: CompletionItemKind.Variable,
        detail: localizer.t("vb.builtin.server.detail"),
      },
      locale,
    ),
    withBuiltinCompletionLabel(
      {
        label: "ASPError",
        kind: CompletionItemKind.Class,
        detail: localizer.t("vb.builtin.asperror.detail"),
      },
      locale,
    ),
    {
      label: "Option Explicit",
      kind: CompletionItemKind.Keyword,
      detail: localizer.t("vb.builtin.optionExplicit.detail"),
    },
    { label: "Dim", kind: CompletionItemKind.Keyword },
    { label: "Set", kind: CompletionItemKind.Keyword },
    { label: "Const", kind: CompletionItemKind.Keyword },
    { label: "Sub", kind: CompletionItemKind.Keyword },
    { label: "Function", kind: CompletionItemKind.Keyword },
    { label: "Class", kind: CompletionItemKind.Keyword },
    ...builtinFunctions.map(
      (item): CompletionItem =>
        withBuiltinCompletionLabel(
          {
            label: item.label,
            kind: CompletionItemKind.Function,
            detail: `Function ${item.signature} As ${item.returnType}`,
            documentation: builtinDocumentationMarkdown(item.documentation, locale),
          },
          locale,
        ),
    ),
    ...builtinConstants.map(
      (item): CompletionItem =>
        withBuiltinCompletionLabel(
          {
            label: item.label,
            kind: CompletionItemKind.Constant,
            detail: `Const ${item.label} As ${item.type}`,
            documentation: builtinDocumentationMarkdown(item.documentation, locale),
          },
          locale,
        ),
    ),
    ...classicAspRuntimeEvents.map(
      (eventSpec): CompletionItem =>
        withBuiltinCompletionLabel(
          {
            label: eventSpec.label,
            kind: CompletionItemKind.Event,
            detail: `Sub ${eventSpec.label}()`,
            documentation: builtinDocumentationMarkdown(eventSpec.documentation, locale),
          },
          locale,
        ),
    ),
  ];
}

function vbscriptSyntaxSnippetCompletions(locale: AspLocale | undefined): CompletionItem[] {
  const detail = createLocalizer(locale).t("vb.completion.syntaxSnippet");
  return [
    snippetCompletion("If Then", "If ${1:condition} Then\n  $0\nEnd If", detail),
    snippetCompletion(
      "If Then Else",
      "If ${1:condition} Then\n  ${2:statement}\nElse\n  $0\nEnd If",
      detail,
    ),
    snippetCompletion("For Next", "For ${1:index} = ${2:start} To ${3:end}\n  $0\nNext", detail),
    snippetCompletion("For Each Next", "For Each ${1:item} In ${2:items}\n  $0\nNext", detail),
    snippetCompletion(
      "Select Case",
      "Select Case ${1:expression}\nCase ${2:value}\n  $0\nEnd Select",
      detail,
    ),
    snippetCompletion("With", "With ${1:object}\n  $0\nEnd With", detail),
    snippetCompletion("Sub", "Sub ${1:Name}(${2:parameters})\n  $0\nEnd Sub", detail),
    snippetCompletion(
      "Function",
      "Function ${1:Name}(${2:parameters})\n  $0\nEnd Function",
      detail,
    ),
    snippetCompletion("Class", "Class ${1:Name}\n  $0\nEnd Class", detail),
    snippetCompletion("Property Get", "Property Get ${1:Name}()\n  $0\nEnd Property", detail),
    snippetCompletion(
      "Property Let",
      "Property Let ${1:Name}(${2:value})\n  $0\nEnd Property",
      detail,
    ),
    snippetCompletion(
      "Property Set",
      "Property Set ${1:Name}(${2:value})\n  $0\nEnd Property",
      detail,
    ),
  ];
}

function snippetCompletion(label: string, insertText: string, detail: string): CompletionItem {
  return {
    label,
    kind: CompletionItemKind.Snippet,
    detail,
    insertText,
    insertTextFormat: InsertTextFormat.Snippet,
  };
}

function withBuiltinCompletionLabel(
  item: CompletionItem,
  locale: AspLocale | undefined,
): CompletionItem {
  return {
    ...item,
    labelDetails: item.labelDetails ?? {
      description: createLocalizer(locale).t("vb.completion.builtinLabel"),
    },
  };
}

function builtinDescription(name: string, locale: AspLocale | undefined): string | undefined {
  const key = `vb.hover.builtin.${name.toLowerCase()}` as const;
  if (
    key === "vb.hover.builtin.request" ||
    key === "vb.hover.builtin.response" ||
    key === "vb.hover.builtin.session" ||
    key === "vb.hover.builtin.application" ||
    key === "vb.hover.builtin.server" ||
    key === "vb.hover.builtin.asperror"
  ) {
    return markdownHover(`Dim ${name} As ${name}`, createLocalizer(locale).t(key));
  }
  const builtin = builtinFunction(name);
  if (builtin) {
    return appendBuiltinDocumentation(
      markdownHover(`Function ${builtin.signature} As ${builtin.returnType}`),
      builtin.documentation,
      locale,
    );
  }
  const constant = builtinConstant(name);
  if (constant) {
    return appendBuiltinDocumentation(
      markdownHover(`Const ${constant.label} As ${constant.type}`),
      constant.documentation,
      locale,
    );
  }
  const runtimeEvent = classicAspRuntimeEvents.find(
    (item) => item.label.toLowerCase() === name.toLowerCase(),
  );
  if (runtimeEvent) {
    return appendBuiltinDocumentation(
      markdownHover(`Sub ${runtimeEvent.label}()`),
      runtimeEvent.documentation,
      locale,
    );
  }
  return undefined;
}

type VbBuiltinMemberKind = VbMember["kind"];

type LocalizedText = {
  en: string;
  ja: string;
};

interface BuiltinParameterSpec {
  name: string;
  type?: string;
  optional?: boolean;
  documentation?: LocalizedText;
}

interface BuiltinDocumentationSpec {
  summary?: LocalizedText;
  remarks?: LocalizedText;
  parameters?: Record<string, LocalizedText>;
  returns?: LocalizedText;
  value?: LocalizedText;
}

interface BuiltinMemberSpec {
  name: string;
  kind: VbBuiltinMemberKind;
  type?: string;
  signature?: string;
  parameters?: BuiltinParameterSpec[];
  documentation?: BuiltinDocumentationSpec;
}

interface BuiltinObjectSpec {
  typeName: string;
  members: BuiltinMemberSpec[];
}

interface BuiltinConstant {
  label: string;
  type: string;
  documentation: BuiltinDocumentationSpec;
}

const classicAspObjectCatalog: Record<string, BuiltinObjectSpec> = {
  request: {
    typeName: "Request",
    members: [
      property("QueryString", "String", "Request.QueryString(name)"),
      property("Form", "String", "Request.Form(name)"),
      property("Cookies", "Variant", "Request.Cookies(name)"),
      property("ServerVariables", "String", "Request.ServerVariables(name)"),
      property("ClientCertificate", "Variant"),
      property("TotalBytes", "Number"),
      method("BinaryRead", "Array", "Request.BinaryRead(count)"),
    ],
  },
  response: {
    typeName: "Response",
    members: [
      property("Cookies"),
      property("Buffer", "Boolean"),
      property("CacheControl", "String"),
      property("Charset", "String"),
      property("ContentType", "String"),
      property("Expires", "Number"),
      property("ExpiresAbsolute", "Date"),
      property("IsClientConnected", "Boolean"),
      property("Pics", "String"),
      property("Status", "String"),
      method("AddHeader", "Variant", "Response.AddHeader(name, value)"),
      method("AppendToLog", "Variant", "Response.AppendToLog string"),
      method("BinaryWrite", "Variant", "Response.BinaryWrite(data)"),
      method("Clear", "Variant", "Response.Clear"),
      method("End", "Variant", "Response.End"),
      method("Flush", "Variant", "Response.Flush"),
      method("Redirect", "Variant", "Response.Redirect url"),
      method("Write", "Variant", "Response.Write value"),
    ],
  },
  application: {
    typeName: "Application",
    members: [
      property("Contents"),
      property("StaticObjects"),
      method("Contents.Remove", "Variant", "Application.Contents.Remove(name)"),
      method("Contents.RemoveAll", "Variant", "Application.Contents.RemoveAll()"),
      method("Lock", "Variant", "Application.Lock"),
      method("Unlock", "Variant", "Application.Unlock"),
    ],
  },
  session: {
    typeName: "Session",
    members: [
      property("Contents"),
      property("StaticObjects"),
      property("CodePage", "Number"),
      property("LCID", "Number"),
      property("SessionID", "String"),
      property("Timeout", "Number"),
      method("Abandon", "Variant", "Session.Abandon"),
      method("Contents.Remove", "Variant", "Session.Contents.Remove(name)"),
      method("Contents.RemoveAll", "Variant", "Session.Contents.RemoveAll()"),
    ],
  },
  server: {
    typeName: "Server",
    members: [
      property("ScriptTimeout", "Number"),
      method("CreateObject", "Object", "Server.CreateObject(progId)"),
      method("Execute", "Variant", "Server.Execute(path)"),
      method("GetLastError", "ASPError", "Server.GetLastError()"),
      method("HTMLEncode", "String", "Server.HTMLEncode(value)"),
      method("MapPath", "String", "Server.MapPath(path)"),
      method("Transfer", "Variant", "Server.Transfer(path)"),
      method("URLEncode", "String", "Server.URLEncode(value)"),
    ],
  },
  asperror: {
    typeName: "ASPError",
    members: [
      property("ASPCode", "String"),
      property("ASPDescription", "String"),
      property("Category", "String"),
      property("Column", "Number"),
      property("Description", "String"),
      property("File", "String"),
      property("Line", "Number"),
      property("Number", "Number"),
      property("Source", "String"),
    ],
  },
};

const externalObjectCatalog: Record<string, BuiltinObjectSpec> = {
  "scripting.filesystemobject": {
    typeName: "Scripting.FileSystemObject",
    members: [
      property("Drives", "Variant"),
      method("BuildPath", "String", "Scripting.FileSystemObject.BuildPath(path, name)"),
      method("CopyFile", "Variant", "Scripting.FileSystemObject.CopyFile(source, destination)"),
      method("CopyFolder", "Variant", "Scripting.FileSystemObject.CopyFolder(source, destination)"),
      method("CreateFolder", "Scripting.Folder", "Scripting.FileSystemObject.CreateFolder(path)"),
      method(
        "CreateTextFile",
        "Scripting.TextStream",
        "Scripting.FileSystemObject.CreateTextFile(filename, overwrite, unicode)",
      ),
      method("DeleteFile", "Variant", "Scripting.FileSystemObject.DeleteFile(fileSpec, force)"),
      method(
        "DeleteFolder",
        "Variant",
        "Scripting.FileSystemObject.DeleteFolder(folderSpec, force)",
      ),
      method("DriveExists", "Boolean", "Scripting.FileSystemObject.DriveExists(driveSpec)"),
      method("FileExists", "Boolean", "Scripting.FileSystemObject.FileExists(fileSpec)"),
      method("FolderExists", "Boolean", "Scripting.FileSystemObject.FolderExists(folderSpec)"),
      method(
        "GetAbsolutePathName",
        "String",
        "Scripting.FileSystemObject.GetAbsolutePathName(pathSpec)",
      ),
      method("GetBaseName", "String", "Scripting.FileSystemObject.GetBaseName(path)"),
      method("GetDrive", "Scripting.Drive", "Scripting.FileSystemObject.GetDrive(driveSpec)"),
      method("GetDriveName", "String", "Scripting.FileSystemObject.GetDriveName(path)"),
      method("GetExtensionName", "String", "Scripting.FileSystemObject.GetExtensionName(path)"),
      method("GetFile", "Scripting.File", "Scripting.FileSystemObject.GetFile(filePath)"),
      method("GetFileName", "String", "Scripting.FileSystemObject.GetFileName(path)"),
      method("GetFolder", "Scripting.Folder", "Scripting.FileSystemObject.GetFolder(path)"),
      method(
        "GetParentFolderName",
        "String",
        "Scripting.FileSystemObject.GetParentFolderName(path)",
      ),
      method(
        "GetSpecialFolder",
        "Scripting.Folder",
        "Scripting.FileSystemObject.GetSpecialFolder(specialFolder)",
      ),
      method("GetTempName", "String", "Scripting.FileSystemObject.GetTempName()"),
      method("MoveFile", "Variant", "Scripting.FileSystemObject.MoveFile(source, destination)"),
      method("MoveFolder", "Variant", "Scripting.FileSystemObject.MoveFolder(source, destination)"),
      method(
        "OpenTextFile",
        "Scripting.TextStream",
        "Scripting.FileSystemObject.OpenTextFile(filename, iomode, create, format)",
      ),
    ],
  },
  "scripting.textstream": {
    typeName: "Scripting.TextStream",
    members: [
      property("AtEndOfLine", "Boolean"),
      property("AtEndOfStream", "Boolean"),
      property("Column", "Number"),
      property("Line", "Number"),
      method("Close", "Variant", "Scripting.TextStream.Close"),
      method("Read", "String", "Scripting.TextStream.Read(characters)"),
      method("ReadAll", "String", "Scripting.TextStream.ReadAll()"),
      method("ReadLine", "String", "Scripting.TextStream.ReadLine()"),
      method("Skip", "Variant", "Scripting.TextStream.Skip(characters)"),
      method("SkipLine", "Variant", "Scripting.TextStream.SkipLine()"),
      method("Write", "Variant", "Scripting.TextStream.Write(text)"),
      method("WriteBlankLines", "Variant", "Scripting.TextStream.WriteBlankLines(lines)"),
      method("WriteLine", "Variant", "Scripting.TextStream.WriteLine(text)"),
    ],
  },
  "scripting.drive": {
    typeName: "Scripting.Drive",
    members: [
      property("AvailableSpace", "Number"),
      property("DriveLetter", "String"),
      property("DriveType", "Number"),
      property("FileSystem", "String"),
      property("FreeSpace", "Number"),
      property("IsReady", "Boolean"),
      property("Path", "String"),
      property("RootFolder", "Scripting.Folder"),
      property("SerialNumber", "Number"),
      property("ShareName", "String"),
      property("TotalSize", "Number"),
      property("VolumeName", "String"),
    ],
  },
  "scripting.file": {
    typeName: "Scripting.File",
    members: [
      property("Attributes", "Number"),
      property("DateCreated", "Date"),
      property("DateLastAccessed", "Date"),
      property("DateLastModified", "Date"),
      property("Drive", "Scripting.Drive"),
      property("Name", "String"),
      property("ParentFolder", "Scripting.Folder"),
      property("Path", "String"),
      property("ShortName", "String"),
      property("ShortPath", "String"),
      property("Size", "Number"),
      property("Type", "String"),
      method("Copy", "Variant", "Scripting.File.Copy(destination, overwrite)"),
      method("Delete", "Variant", "Scripting.File.Delete(force)"),
      method("Move", "Variant", "Scripting.File.Move(destination)"),
      method(
        "OpenAsTextStream",
        "Scripting.TextStream",
        "Scripting.File.OpenAsTextStream(iomode, format)",
      ),
    ],
  },
  "scripting.folder": {
    typeName: "Scripting.Folder",
    members: [
      property("Files", "Variant"),
      property("SubFolders", "Variant"),
      property("Attributes", "Number"),
      property("DateCreated", "Date"),
      property("DateLastAccessed", "Date"),
      property("DateLastModified", "Date"),
      property("Drive", "Scripting.Drive"),
      property("IsRootFolder", "Boolean"),
      property("Name", "String"),
      property("ParentFolder", "Scripting.Folder"),
      property("Path", "String"),
      property("ShortName", "String"),
      property("ShortPath", "String"),
      property("Size", "Number"),
      property("Type", "String"),
      method("Copy", "Variant", "Scripting.Folder.Copy(destination, overwrite)"),
      method(
        "CreateTextFile",
        "Scripting.TextStream",
        "Scripting.Folder.CreateTextFile(filename, overwrite, unicode)",
      ),
      method("Delete", "Variant", "Scripting.Folder.Delete(force)"),
      method("Move", "Variant", "Scripting.Folder.Move(destination)"),
    ],
  },
  "scripting.dictionary": {
    typeName: "Scripting.Dictionary",
    members: [
      property("CompareMode", "Number"),
      property("Count", "Number"),
      property("Item", "Variant"),
      property("Key", "Variant"),
      method("Add", "Variant", "Scripting.Dictionary.Add(key, item)"),
      method("Exists", "Boolean", "Scripting.Dictionary.Exists(key)"),
      method("Items", "Array", "Scripting.Dictionary.Items()"),
      method("Keys", "Array", "Scripting.Dictionary.Keys()"),
      method("Remove", "Variant", "Scripting.Dictionary.Remove(key)"),
      method("RemoveAll", "Variant", "Scripting.Dictionary.RemoveAll()"),
    ],
  },
  "mswc.adrotator": {
    typeName: "MSWC.AdRotator",
    members: [
      property("Border", "Number"),
      property("Clickable", "Boolean"),
      property("TargetFrame", "String"),
      method("GetAdvertisement", "String", "MSWC.AdRotator.GetAdvertisement(scheduleFile)"),
    ],
  },
  "mswc.browsertype": {
    typeName: "MSWC.BrowserType",
    members: [
      property("ActiveXControls", "Boolean"),
      property("Backgroundsounds", "Boolean"),
      property("Beta", "Boolean"),
      property("Browser", "String"),
      property("Cdf", "Boolean"),
      property("Cookies", "Boolean"),
      property("Frames", "Boolean"),
      property("Javaapplets", "Boolean"),
      property("Javascript", "Boolean"),
      property("MajorVer", "Number"),
      property("MinorVer", "Number"),
      property("Platform", "String"),
      property("Tables", "Boolean"),
      property("Vbscript", "Boolean"),
      property("Version", "String"),
    ],
  },
  "mswc.nextlink": {
    typeName: "MSWC.NextLink",
    members: [
      method("GetListCount", "Number", "MSWC.NextLink.GetListCount(listFile)"),
      method("GetListIndex", "Number", "MSWC.NextLink.GetListIndex(listFile)"),
      method("GetNextDescription", "String", "MSWC.NextLink.GetNextDescription(listFile)"),
      method("GetNextURL", "String", "MSWC.NextLink.GetNextURL(listFile)"),
      method("GetNthDescription", "String", "MSWC.NextLink.GetNthDescription(listFile, index)"),
      method("GetNthURL", "String", "MSWC.NextLink.GetNthURL(listFile, index)"),
      method("GetPreviousDescription", "String", "MSWC.NextLink.GetPreviousDescription(listFile)"),
      method("GetPreviousURL", "String", "MSWC.NextLink.GetPreviousURL(listFile)"),
    ],
  },
  "mswc.contentrotator": {
    typeName: "MSWC.ContentRotator",
    members: [
      method("ChooseContent", "String", "MSWC.ContentRotator.ChooseContent(contentSchedule)"),
      method("GetAllContent", "String", "MSWC.ContentRotator.GetAllContent(contentSchedule)"),
    ],
  },
  "adodb.command": {
    typeName: "ADODB.Command",
    members: [
      property("ActiveConnection", "Object"),
      property("CommandText", "String"),
      property("CommandTimeout", "Number"),
      property("CommandType", "Number"),
      property("Name", "String"),
      property("Prepared", "Boolean"),
      property("State", "Number"),
      method("Cancel", "Variant", "ADODB.Command.Cancel()"),
      method(
        "CreateParameter",
        "ADODB.Parameter",
        "ADODB.Command.CreateParameter(name, type, direction, size, value)",
      ),
      method(
        "Execute",
        "ADODB.Recordset",
        "ADODB.Command.Execute(recordsAffected, parameters, options)",
      ),
      property("Parameters", "Object"),
      property("Properties", "Object"),
    ],
  },
  "adodb.connection": {
    typeName: "ADODB.Connection",
    members: [
      property("Attributes", "Number"),
      property("CommandTimeout", "Number"),
      property("ConnectionString", "String"),
      property("ConnectionTimeout", "Number"),
      property("CursorLocation", "Number"),
      property("DefaultDatabase", "String"),
      property("IsolationLevel", "Number"),
      property("Mode", "Number"),
      property("Provider", "String"),
      property("State", "Number"),
      property("Version", "String"),
      method("BeginTrans", "Number", "ADODB.Connection.BeginTrans()"),
      method("Cancel", "Variant", "ADODB.Connection.Cancel()"),
      method("Close", "Variant", "ADODB.Connection.Close()"),
      method("CommitTrans", "Variant", "ADODB.Connection.CommitTrans()"),
      method(
        "Execute",
        "ADODB.Recordset",
        "ADODB.Connection.Execute(commandText, recordsAffected, options)",
      ),
      method(
        "Open",
        "Variant",
        "ADODB.Connection.Open(connectionString, userId, password, options)",
      ),
      method(
        "OpenSchema",
        "ADODB.Recordset",
        "ADODB.Connection.OpenSchema(schema, restrictions, schemaId)",
      ),
      method("RollbackTrans", "Variant", "ADODB.Connection.RollbackTrans()"),
      event("BeginTransComplete"),
      event("CommitTransComplete"),
      event("ConnectComplete"),
      event("Disconnect"),
      event("ExecuteComplete"),
      event("InfoMessage"),
      event("RollbackTransComplete"),
      event("WillConnect"),
      event("WillExecute"),
      property("Errors", "Object"),
      property("Properties", "Object"),
    ],
  },
  "adodb.error": {
    typeName: "ADODB.Error",
    members: [
      property("Description", "String"),
      property("HelpContext", "Number"),
      property("HelpFile", "String"),
      property("NativeError", "Number"),
      property("Number", "Number"),
      property("Source", "String"),
      property("SQLState", "String"),
    ],
  },
  "adodb.field": {
    typeName: "ADODB.Field",
    members: [
      property("ActualSize", "Number"),
      property("Attributes", "Number"),
      property("DefinedSize", "Number"),
      property("Name", "String"),
      property("NumericScale", "Number"),
      property("OriginalValue", "Variant"),
      property("Precision", "Number"),
      property("Status", "Number"),
      property("Type", "Number"),
      property("UnderlyingValue", "Variant"),
      property("Value", "Variant"),
      method("AppendChunk", "Variant", "ADODB.Field.AppendChunk(data)"),
      method("GetChunk", "Variant", "ADODB.Field.GetChunk(length)"),
      property("Properties", "Object"),
    ],
  },
  "adodb.parameter": {
    typeName: "ADODB.Parameter",
    members: [
      property("Attributes", "Number"),
      property("Direction", "Number"),
      property("Name", "String"),
      property("NumericScale", "Number"),
      property("Precision", "Number"),
      property("Size", "Number"),
      property("Type", "Number"),
      property("Value", "Variant"),
      method("AppendChunk", "Variant", "ADODB.Parameter.AppendChunk(value)"),
      method("Delete", "Variant", "ADODB.Parameter.Delete()"),
    ],
  },
  "adodb.property": {
    typeName: "ADODB.Property",
    members: [
      property("Attributes", "Number"),
      property("Name", "String"),
      property("Type", "Number"),
      property("Value", "Variant"),
    ],
  },
  "adodb.record": {
    typeName: "ADODB.Record",
    members: [
      property("ActiveConnection", "Object"),
      property("Mode", "Number"),
      property("ParentURL", "String"),
      property("RecordType", "Number"),
      property("Source", "Variant"),
      property("State", "Number"),
      method("Cancel", "Variant", "ADODB.Record.Cancel()"),
      method("Close", "Variant", "ADODB.Record.Close()"),
      method(
        "CopyRecord",
        "String",
        "ADODB.Record.CopyRecord(source, destination, userName, password, options, async)",
      ),
      method("DeleteRecord", "Variant", "ADODB.Record.DeleteRecord(source, async)"),
      method("GetChildren", "ADODB.Recordset", "ADODB.Record.GetChildren()"),
      method(
        "MoveRecord",
        "String",
        "ADODB.Record.MoveRecord(source, destination, userName, password, options, async)",
      ),
      method(
        "Open",
        "Variant",
        "ADODB.Record.Open(source, activeConnection, mode, createOptions, options, userName, password)",
      ),
      property("Properties", "Object"),
      property("Fields", "Object"),
    ],
  },
  "adodb.recordset": {
    typeName: "ADODB.Recordset",
    members: [
      property("AbsolutePage", "Number"),
      property("AbsolutePosition", "Number"),
      property("ActiveCommand", "ADODB.Command"),
      property("ActiveConnection", "Object"),
      property("BOF", "Boolean"),
      property("Bookmark", "Variant"),
      property("CacheSize", "Number"),
      property("CursorLocation", "Number"),
      property("CursorType", "Number"),
      property("DataMember", "String"),
      property("DataSource", "Object"),
      property("EditMode", "Number"),
      property("EOF", "Boolean"),
      property("Filter", "Variant"),
      property("Index", "String"),
      property("LockType", "Number"),
      property("MarshalOptions", "Number"),
      property("MaxRecords", "Number"),
      property("PageCount", "Number"),
      property("PageSize", "Number"),
      property("RecordCount", "Number"),
      property("Sort", "String"),
      property("Source", "Variant"),
      property("State", "Number"),
      property("Status", "Number"),
      property("StayInSync", "Boolean"),
      method("AddNew", "Variant", "ADODB.Recordset.AddNew(fieldList, values)"),
      method("Cancel", "Variant", "ADODB.Recordset.Cancel()"),
      method("CancelBatch", "Variant", "ADODB.Recordset.CancelBatch(affectRecords)"),
      method("CancelUpdate", "Variant", "ADODB.Recordset.CancelUpdate()"),
      method("Clone", "ADODB.Recordset", "ADODB.Recordset.Clone(lockType)"),
      method("Close", "Variant", "ADODB.Recordset.Close()"),
      method(
        "CompareBookmarks",
        "Number",
        "ADODB.Recordset.CompareBookmarks(bookmark1, bookmark2)",
      ),
      method("Delete", "Variant", "ADODB.Recordset.Delete(affectRecords)"),
      method(
        "Find",
        "Variant",
        "ADODB.Recordset.Find(criteria, skipRecords, searchDirection, start)",
      ),
      method("GetRows", "Array", "ADODB.Recordset.GetRows(rows, start, fields)"),
      method(
        "GetString",
        "String",
        "ADODB.Recordset.GetString(stringFormat, numRows, columnDelimiter, rowDelimiter, nullExpr)",
      ),
      method("Move", "Variant", "ADODB.Recordset.Move(numRecords, start)"),
      method("MoveFirst", "Variant", "ADODB.Recordset.MoveFirst()"),
      method("MoveLast", "Variant", "ADODB.Recordset.MoveLast()"),
      method("MoveNext", "Variant", "ADODB.Recordset.MoveNext()"),
      method("MovePrevious", "Variant", "ADODB.Recordset.MovePrevious()"),
      method("NextRecordset", "ADODB.Recordset", "ADODB.Recordset.NextRecordset(recordsAffected)"),
      method(
        "Open",
        "Variant",
        "ADODB.Recordset.Open(source, activeConnection, cursorType, lockType, options)",
      ),
      method("Requery", "Variant", "ADODB.Recordset.Requery(options)"),
      method("Resync", "Variant", "ADODB.Recordset.Resync(affectRecords, resyncValues)"),
      method("Save", "Variant", "ADODB.Recordset.Save(destination, persistFormat)"),
      method("Seek", "Variant", "ADODB.Recordset.Seek(keyValues, seekOption)"),
      method("Supports", "Boolean", "ADODB.Recordset.Supports(cursorOptions)"),
      method("Update", "Variant", "ADODB.Recordset.Update(fields, values)"),
      method("UpdateBatch", "Variant", "ADODB.Recordset.UpdateBatch(affectRecords)"),
      event("EndOfRecordset"),
      event("FetchComplete"),
      event("FetchProgress"),
      event("FieldChangeComplete"),
      event("MoveComplete"),
      event("RecordChangeComplete"),
      event("RecordsetChangeComplete"),
      event("WillChangeField"),
      event("WillChangeRecord"),
      event("WillChangeRecordset"),
      event("WillMove"),
      property("Fields", "Object"),
      property("Properties", "Object"),
    ],
  },
  "adodb.stream": {
    typeName: "ADODB.Stream",
    members: [
      property("CharSet", "String"),
      property("EOS", "Boolean"),
      property("LineSeparator", "Number"),
      property("Mode", "Number"),
      property("Position", "Number"),
      property("Size", "Number"),
      property("State", "Number"),
      property("Type", "Number"),
      method("Cancel", "Variant", "ADODB.Stream.Cancel()"),
      method("Close", "Variant", "ADODB.Stream.Close()"),
      method("CopyTo", "Variant", "ADODB.Stream.CopyTo(destination, charNumber)"),
      method("Flush", "Variant", "ADODB.Stream.Flush()"),
      method("LoadFromFile", "Variant", "ADODB.Stream.LoadFromFile(filename)"),
      method("Open", "Variant", "ADODB.Stream.Open(source, mode, openOptions, userName, password)"),
      method("Read", "Variant", "ADODB.Stream.Read(numBytes)"),
      method("ReadText", "String", "ADODB.Stream.ReadText(numChars)"),
      method("SaveToFile", "Variant", "ADODB.Stream.SaveToFile(filename, saveOptions)"),
      method("SetEOS", "Variant", "ADODB.Stream.SetEOS()"),
      method("SkipLine", "Variant", "ADODB.Stream.SkipLine()"),
      method("Write", "Variant", "ADODB.Stream.Write(buffer)"),
      method("WriteText", "Variant", "ADODB.Stream.WriteText(data, options)"),
    ],
  },
};

const classicAspBuiltinSignatures = objectSignatures(classicAspObjectCatalog);
const memberCompletions = objectCompletions(classicAspObjectCatalog);
const externalObjectMembers = objectCompletions(externalObjectCatalog);

const intrinsicTypeNames = new Set([
  "array",
  "byte",
  "string",
  "integer",
  "long",
  "single",
  "double",
  "currency",
  "decimal",
  "number",
  "boolean",
  "date",
  "empty",
  "null",
  "variant",
  "unknown",
  "nothing",
  "error",
]);

const classicAspTypeNames = new Set([
  "request",
  "response",
  "session",
  "application",
  "server",
  "asperror",
]);

const builtinConstants: BuiltinConstant[] = [
  "adBigInt",
  "adBinary",
  "adBoolean",
  "adChar",
  "adCurrency",
  "adDate",
  "adDBTimeStamp",
  "adDecimal",
  "adDouble",
  "adGUID",
  "adIDispatch",
  "adInteger",
  "adLongVarBinary",
  "adLongVarChar",
  "adLongVarWChar",
  "adNumeric",
  "adSingle",
  "adSmallInt",
  "adUnsignedTinyInt",
  "adVarBinary",
  "adVarChar",
  "adVariant",
  "adVarWChar",
  "adWChar",
].map((label) => ({
  label,
  type: "Number",
  documentation: builtinConstantDocumentation(label),
}));

const classicAspRuntimeEvents = [
  {
    label: "Application_OnStart",
    documentation: documentation(
      "Runs when the ASP application starts before the first session is created.",
      "ASP application が開始し、最初の session が作られる前に実行されます。",
      {
        remarks: text(
          "Define this event in Global.asa for application-wide initialization.",
          "Global.asa に定義し、application 全体の初期化に使います。",
        ),
      },
    ),
  },
  {
    label: "Application_OnEnd",
    documentation: documentation(
      "Runs when the ASP application ends.",
      "ASP application が終了するときに実行されます。",
      {
        remarks: text(
          "Use this event for application-wide cleanup that does not depend on an active response.",
          "有効な response に依存しない application 全体の後始末に使います。",
        ),
      },
    ),
  },
  {
    label: "Session_OnStart",
    documentation: documentation(
      "Runs when a new user session starts.",
      "新しい user session が開始するときに実行されます。",
      {
        remarks: text(
          "Define this event in Global.asa to initialize per-user session state.",
          "Global.asa に定義し、user ごとの session state を初期化します。",
        ),
      },
    ),
  },
  {
    label: "Session_OnEnd",
    documentation: documentation(
      "Runs when a user session ends or times out.",
      "user session が終了するか timeout したときに実行されます。",
      {
        remarks: text(
          "Use this event for session cleanup; response output is not available.",
          "session の後始末に使います。response output は利用できません。",
        ),
      },
    ),
  },
];

function property(name: string, type = "Variant", signature?: string): BuiltinMemberSpec {
  return builtinMember("property", name, type, signature);
}

function method(name: string, type = "Variant", signature?: string): BuiltinMemberSpec {
  return builtinMember("method", name, type, signature);
}

function event(name: string): BuiltinMemberSpec {
  return builtinMember("event", name, "Variant");
}

function builtinMember(
  kind: VbBuiltinMemberKind,
  name: string,
  type = "Variant",
  signature?: string,
): BuiltinMemberSpec {
  return {
    name,
    kind,
    type,
    signature,
    parameters: signature ? parametersFromSignature(signature).map(completeParameterSpec) : [],
    documentation: builtinMemberDocumentation(kind, name, type, signature),
  };
}

function text(en: string, ja: string): LocalizedText {
  return { en, ja };
}

function documentation(
  en: string,
  ja: string,
  extra: Omit<BuiltinDocumentationSpec, "summary"> = {},
): BuiltinDocumentationSpec {
  return { summary: text(en, ja), ...extra };
}

function localizedText(value: LocalizedText | undefined, locale: AspLocale | undefined): string {
  if (!value) {
    return "";
  }
  return createLocalizer(locale).locale === "ja" ? value.ja : value.en;
}

function parametersFromSignature(signature: string): BuiltinParameterSpec[] {
  const parameterText =
    /\((.*)\)/.exec(signature)?.[1] ?? signature.split(/\s+/).slice(1).join(", ");
  return parameterText
    ? parameterText
        .split(",")
        .map((part) => part.trim())
        .filter(Boolean)
        .map((parameter) => ({ name: parameter.replace(/^\[|\]$/g, "") }))
    : [];
}

function completeParameterSpec(parameter: BuiltinParameterSpec): BuiltinParameterSpec {
  return {
    ...parameter,
    documentation: parameter.documentation ?? genericParameterDocumentation(parameter.name),
  };
}

function genericParameterDocumentation(name: string): LocalizedText {
  const normalized = name.toLowerCase();
  const docs: Record<string, LocalizedText> = {
    activeconnection: text(
      "Connection object or connection string used by the operation.",
      "操作に使う Connection object または connection string です。",
    ),
    affectrecords: text(
      "Affects which records are included in the operation.",
      "操作の対象にする records の範囲を指定します。",
    ),
    array: text("Array value to inspect.", "調べる array value です。"),
    async: text(
      "Controls whether the operation is asynchronous.",
      "操作を asynchronous にするかを指定します。",
    ),
    buffer: text(
      "Binary data buffer passed to the stream.",
      "stream に渡す binary data buffer です。",
    ),
    characters: text(
      "Number of characters to read or skip.",
      "読み取る、または skip する文字数です。",
    ),
    charnumber: text("Number of characters to copy.", "copy する文字数です。"),
    commandtext: text(
      "Command text or query to execute.",
      "実行する command text または query です。",
    ),
    compare: text(
      "Optional comparison mode for string matching.",
      "文字列比較に使う任意の comparison mode です。",
    ),
    connectionstring: text(
      "Connection string used to open the connection.",
      "connection を開く connection string です。",
    ),
    contentschedule: text(
      "Path to the content schedule file.",
      "content schedule file への path です。",
    ),
    count: text("Maximum number of items to process.", "処理する item の最大数です。"),
    criteria: text("Search criteria used by the operation.", "検索に使う criteria です。"),
    cursoroptions: text(
      "Cursor feature to test for support.",
      "support を確認する cursor feature です。",
    ),
    cursortype: text(
      "Cursor type used when opening the recordset.",
      "recordset を開く cursor type です。",
    ),
    data: text("Data value passed to the method.", "method に渡す data value です。"),
    date: text("Date expression to evaluate.", "評価する date expression です。"),
    date1: text(
      "First date expression in the comparison.",
      "比較する 1 つ目の date expression です。",
    ),
    date2: text(
      "Second date expression in the comparison.",
      "比較する 2 つ目の date expression です。",
    ),
    decimalplaces: text("Number of decimal places to keep.", "保持する小数点以下の桁数です。"),
    delimiter: text(
      "String used to separate or join values.",
      "値の分割または結合に使う区切り文字列です。",
    ),
    destination: text(
      "Destination path or object for the operation.",
      "操作先の path または object です。",
    ),
    digitsafterdecimal: text(
      "Number of digits to display after the decimal point.",
      "小数点以下に表示する桁数です。",
    ),
    dimension: text("Array dimension to inspect.", "調べる array dimension です。"),
    direction: text("ADO parameter direction value.", "ADO parameter の direction value です。"),
    expression: text(
      "Expression to evaluate or format.",
      "評価または format する expression です。",
    ),
    fieldlist: text(
      "Field name, field number, or list of fields.",
      "field name、field number、または fields の list です。",
    ),
    fields: text(
      "Field name, field number, or list of fields.",
      "field name、field number、または fields の list です。",
    ),
    filename: text(
      "File name or path used by the operation.",
      "操作に使う file name または path です。",
    ),
    filespec: text(
      "File specification or path pattern.",
      "file specification または path pattern です。",
    ),
    find: text("Text to search for.", "検索する text です。"),
    firstdayofweek: text(
      "Optional first-day-of-week setting for date calculations.",
      "date calculation に使う任意の first-day-of-week 設定です。",
    ),
    firstweekofyear: text(
      "Optional first-week-of-year setting for date calculations.",
      "date calculation に使う任意の first-week-of-year 設定です。",
    ),
    flag: text(
      "Boolean flag that controls the property.",
      "property を制御する Boolean flag です。",
    ),
    folderspec: text(
      "Folder specification or path pattern.",
      "folder specification または path pattern です。",
    ),
    force: text(
      "Boolean value that allows read-only items to be changed or deleted.",
      "read-only item の変更や削除を許可する Boolean value です。",
    ),
    format: text("Format option used by the method.", "method に使う format option です。"),
    groupdigits: text("Controls whether digits are grouped.", "桁区切りを使うかを指定します。"),
    include: text(
      "Controls whether matching entries are included or excluded.",
      "一致した entry を含めるか除外するかを指定します。",
    ),
    includeleadingdigit: text(
      "Controls whether a leading zero is displayed for fractional values.",
      "1 未満の値に先頭の 0 を表示するかを指定します。",
    ),
    index: text(
      "One-based or zero-based index used by the component.",
      "component が使う index です。",
    ),
    inputstrings: text("Array of strings to filter.", "filter する string array です。"),
    interval: text(
      "Date interval code such as yyyy, q, m, d, h, n, or s.",
      "yyyy、q、m、d、h、n、s などの date interval code です。",
    ),
    iomode: text("File input/output mode.", "file の input/output mode です。"),
    key: text("Dictionary key or lookup key.", "Dictionary key または lookup key です。"),
    keyvalues: text(
      "Key value or array of key values for the seek operation.",
      "seek operation に使う key value または key values の array です。",
    ),
    length: text(
      "Number of characters, bytes, or items to use.",
      "使う文字数、bytes、または items 数です。",
    ),
    list: text("Array or list value to process.", "処理する array または list value です。"),
    listfile: text(
      "Path to the list file used by the component.",
      "component が使う list file への path です。",
    ),
    locktype: text("Lock type used by the recordset.", "recordset に使う lock type です。"),
    name: text("Name to read, create, or update.", "読み取り、作成、または更新する name です。"),
    namedformat: text("Named date/time format value.", "date/time の named format value です。"),
    number: text("Numeric value used by the function.", "function に使う numeric value です。"),
    numbytes: text("Number of bytes to read.", "読み取る bytes 数です。"),
    numchars: text("Number of characters to read.", "読み取る文字数です。"),
    numrecords: text("Number of records to move.", "移動する records 数です。"),
    numrows: text("Number of rows to process.", "処理する rows 数です。"),
    options: text(
      "Provider-specific options for the operation.",
      "operation に使う provider-specific options です。",
    ),
    overwrite: text(
      "Boolean value that allows an existing item to be overwritten.",
      "既存 item の上書きを許可する Boolean value です。",
    ),
    parameter: text(
      "Parameter value passed to the method.",
      "method に渡す parameter value です。",
    ),
    parameters: text(
      "ADO parameters passed to the command.",
      "command に渡す ADO parameters です。",
    ),
    password: text("Password used to open the resource.", "resource を開く password です。"),
    path: text(
      "Path to resolve, execute, or transfer to.",
      "解決、実行、または transfer する path です。",
    ),
    pathspec: text("Path specification to convert.", "変換する path specification です。"),
    persistformat: text(
      "Persistence format used when saving data.",
      "data 保存時に使う persistence format です。",
    ),
    recordsaffected: text(
      "Variable that receives the number of affected records.",
      "affected records 数を受け取る variable です。",
    ),
    replacewith: text("Replacement text to insert.", "挿入する replacement text です。"),
    restrictions: text(
      "Schema restrictions for the query.",
      "schema query に使う restrictions です。",
    ),
    rows: text("Number of records to retrieve.", "取得する records 数です。"),
    saveoptions: text("Options used when saving the stream.", "stream 保存時に使う options です。"),
    schedulefile: text(
      "Path to the advertisement schedule file.",
      "advertisement schedule file への path です。",
    ),
    schema: text("Schema query type.", "schema query type です。"),
    schemaid: text("Provider schema identifier.", "provider schema identifier です。"),
    searchdirection: text(
      "Direction used when searching records.",
      "records 検索時の direction です。",
    ),
    seekoption: text("Seek option used by the recordset.", "recordset が使う seek option です。"),
    size: text(
      "Size assigned to the parameter or field.",
      "parameter または field に設定する size です。",
    ),
    source: text(
      "Source path, command, record, or data object.",
      "source path、command、record、または data object です。",
    ),
    specialfolder: text(
      "Special folder constant to resolve.",
      "解決する special folder constant です。",
    ),
    start: text("Start position, record, or bookmark.", "開始位置、record、または bookmark です。"),
    string1: text("First string expression.", "1 つ目の string expression です。"),
    string2: text("Second string expression.", "2 つ目の string expression です。"),
    stringformat: text(
      "Format used when converting the recordset to text.",
      "recordset を text に変換するときの format です。",
    ),
    text: text("Text value passed to the method.", "method に渡す text value です。"),
    type: text("ADO data type value.", "ADO data type value です。"),
    unicode: text(
      "Boolean value that controls Unicode file output.",
      "Unicode file output を使うかを指定する Boolean value です。",
    ),
    userid: text("User ID used to open the connection.", "connection を開く user ID です。"),
    username: text("User name used to open the resource.", "resource を開く user name です。"),
    useparensfornegativenumbers: text(
      "Controls whether negative numbers are wrapped in parentheses.",
      "negative numbers を parentheses で囲むかを指定します。",
    ),
    value: text(
      "Value to convert, evaluate, assign, or write.",
      "変換、評価、代入、または書き込みする value です。",
    ),
    values: text(
      "Values to place in the array or fields.",
      "array または fields に入れる values です。",
    ),
  };
  return (
    docs[normalized] ??
    text(`Value supplied for the ${name} argument.`, `${name} argument に渡す value です。`)
  );
}

function builtinMemberDocumentation(
  kind: VbBuiltinMemberKind,
  name: string,
  type: string,
  signature: string | undefined,
): BuiltinDocumentationSpec {
  const key = signature?.toLowerCase() ?? name.toLowerCase();
  const overrides: Record<string, BuiltinDocumentationSpec> = {
    buffer: documentation(
      "Controls whether ASP buffers page output before sending it to the browser.",
      "ASP が browser へ送る前に page output を buffer するかを制御します。",
      {
        value: text(
          "Boolean. True holds output until scripts finish or Response.Flush/Response.End is called; False streams output as it is processed.",
          "Boolean です。True は scripts の完了または Response.Flush / Response.End まで output を保持し、False は処理に合わせて output を送ります。",
        ),
        remarks: text(
          "Set this before the html tag when the page needs explicit buffering behavior.",
          "page の buffering behavior を明示するときは html tag より前で設定します。",
        ),
      },
    ),
    "server.execute(path)": documentation(
      "Runs another ASP page and returns control to the current script when it completes.",
      "別の ASP page を実行し、完了後に現在の script へ制御を戻します。",
      {
        parameters: {
          path: text(
            "Relative or absolute path of the ASP page to execute.",
            "実行する ASP page の relative または absolute path です。",
          ),
        },
        returns: text(
          "No meaningful value is returned; the executed page can write to the response.",
          "意味のある値は返しません。実行された page は response に書き込めます。",
        ),
      },
    ),
    "server.createobject(progid)": documentation(
      "Creates an instance of a registered COM component by Prog.ID.",
      "Prog.ID で登録済み COM component の instance を作成します。",
      {
        parameters: {
          progid: text(
            "Programmatic identifier such as Scripting.FileSystemObject or ADODB.Recordset.",
            "Scripting.FileSystemObject や ADODB.Recordset などの programmatic identifier です。",
          ),
        },
        returns: text("The created COM object.", "作成された COM object です。"),
      },
    ),
    "adodb.recordset.getrows(rows, start, fields)": documentation(
      "Copies records from a Recordset into a two-dimensional array.",
      "Recordset から records を 2 次元 array へ copy します。",
      {
        parameters: {
          rows: text(
            "Optional count of records to retrieve. Omitting it retrieves the rest of the Recordset.",
            "取得する records 数です。省略すると Recordset の残りを取得します。",
          ),
          start: text(
            "Optional record number or bookmark where copying starts.",
            "copy を開始する record number または bookmark です。",
          ),
          fields: text(
            "Optional single field name/number or array of field names/numbers to include.",
            "含める field name/number、または field names/numbers の array です。",
          ),
        },
        returns: text(
          "A two-dimensional array indexed by field and row.",
          "field と row で参照する 2 次元 array を返します。",
        ),
      },
    ),
  };
  const generic =
    kind === "event"
      ? documentation(
          `${name} event on the ${type} object.`,
          `${type} object の ${name} event です。`,
        )
      : kind === "method"
        ? documentation(`Calls the ${name} method.`, `${name} method を呼び出します。`, {
            parameters: parametersForDocumentation(signature),
            returns: text(`Returns ${type}.`, `${type} を返します。`),
          })
        : documentation(
            `Represents the ${name} ${kind} on this object.`,
            `この object の ${name} ${kind} を表します。`,
            { value: text(`Value type: ${type}.`, `値の型は ${type} です。`) },
          );
  return overrides[key] ?? generic;
}

function parametersForDocumentation(
  signature: string | undefined,
): Record<string, LocalizedText> | undefined {
  if (!signature) {
    return undefined;
  }
  const parameters = parametersFromSignature(signature);
  return parameters.length > 0
    ? Object.fromEntries(
        parameters.map((parameter) => [
          parameter.name.toLowerCase(),
          genericParameterDocumentation(parameter.name),
        ]),
      )
    : undefined;
}

function builtinConstantDocumentation(label: string): BuiltinDocumentationSpec {
  const lower = label.toLowerCase();
  const descriptions: Record<string, LocalizedText> = {
    adinteger: text(
      "ADO data type constant for a 32-bit signed integer value.",
      "32-bit signed integer value を表す ADO data type constant です。",
    ),
    advarchar: text(
      "ADO data type constant for a variable-length non-Unicode string.",
      "variable-length non-Unicode string を表す ADO data type constant です。",
    ),
    advarwchar: text(
      "ADO data type constant for a variable-length Unicode string.",
      "variable-length Unicode string を表す ADO data type constant です。",
    ),
    adboolean: text(
      "ADO data type constant for a Boolean value.",
      "Boolean value を表す ADO data type constant です。",
    ),
    addate: text(
      "ADO data type constant for a date value.",
      "date value を表す ADO data type constant です。",
    ),
  };
  const summary =
    descriptions[lower] ??
    text(
      `ADO data type constant used when declaring fields or parameters as ${label}.`,
      `fields や parameters を ${label} として扱う ADO data type constant です。`,
    );
  return {
    summary,
    value: text(
      "Numeric ADO DataTypeEnum constant.",
      "numeric な ADO DataTypeEnum constant です。",
    ),
  };
}

function builtinFunctionDocumentation(
  label: string,
  summary: string,
  returnType: string,
  signature: string,
): BuiltinDocumentationSpec {
  const override = builtinFunctionDocumentationOverride(label);
  return {
    summary: override.summary ?? text(summary, `${label} 関数です。${returnType} を返します。`),
    remarks: override.remarks,
    parameters: Object.fromEntries(
      parametersFromSignature(signature).map((parameter) => [
        parameter.name.toLowerCase(),
        override.parameters?.[parameter.name.toLowerCase()] ??
          genericParameterDocumentation(parameter.name),
      ]),
    ),
    returns: override.returns ?? text(`Returns ${returnType}.`, `${returnType} を返します。`),
    value: override.value,
  };
}

function builtinFunctionDocumentationOverride(label: string): BuiltinDocumentationSpec {
  const overrides: Record<string, BuiltinDocumentationSpec> = {
    createobject: documentation(
      "Creates an instance of a registered automation object by Prog.ID.",
      "Prog.ID で登録済み automation object の instance を作成します。",
      {
        parameters: {
          progid: text(
            "Programmatic identifier such as Scripting.Dictionary or ADODB.Stream.",
            "Scripting.Dictionary や ADODB.Stream などの programmatic identifier です。",
          ),
        },
        returns: text("The created automation object.", "作成された automation object です。"),
      },
    ),
    datepart: documentation(
      "Returns the requested interval part from a date expression.",
      "date expression から指定した interval part を返します。",
      {
        parameters: {
          interval: text(
            "Interval code to return, such as yyyy, q, m, d, w, ww, h, n, or s.",
            "返す interval code です。yyyy、q、m、d、w、ww、h、n、s などを指定します。",
          ),
          date: text("Date expression to evaluate.", "評価する date expression です。"),
          firstdayofweek: text(
            "Optional first day of the week used for weekday and week calculations.",
            "weekday や week calculation に使う任意の first day of week です。",
          ),
          firstweekofyear: text(
            "Optional rule that chooses which week counts as the first week of the year.",
            "year の first week を決める任意の rule です。",
          ),
        },
        returns: text(
          "Number containing the selected part of the date.",
          "date の指定部分を表す Number を返します。",
        ),
        remarks: text(
          "Week-related intervals use firstDayOfWeek and firstWeekOfYear when supplied.",
          "week 関連の interval では、指定された firstDayOfWeek と firstWeekOfYear を使います。",
        ),
      },
    ),
    formatcurrency: documentation(
      "Formats a numeric expression as a currency string.",
      "numeric expression を currency string として format します。",
      {
        returns: text("Formatted currency string.", "format 済み currency string を返します。"),
      },
    ),
    split: documentation(
      "Splits a string into a zero-based array of substrings.",
      "string を 0-based の substring array に分割します。",
      {
        returns: text("Array of substrings.", "substrings の array を返します。"),
      },
    ),
    join: documentation(
      "Combines array items into one string using a delimiter.",
      "array items を delimiter で 1 つの string に結合します。",
      {
        returns: text("Joined string.", "結合された string を返します。"),
      },
    ),
    isobject: documentation(
      "Returns whether an expression references an automation object.",
      "expression が automation object を参照しているかを返します。",
      {
        returns: text("Boolean result.", "Boolean result を返します。"),
      },
    ),
    vartype: documentation(
      "Returns a numeric code for the VBScript subtype of an expression.",
      "expression の VBScript subtype を表す numeric code を返します。",
      {
        returns: text("Numeric subtype code.", "numeric subtype code を返します。"),
      },
    ),
  };
  return overrides[label.toLowerCase()] ?? {};
}

function objectSignatures(catalog: Record<string, BuiltinObjectSpec>): Record<string, string[]> {
  return Object.fromEntries(
    Object.entries(catalog).flatMap(([objectName, objectSpec]) =>
      objectSpec.members.flatMap((member) =>
        member.signature
          ? [[`${objectName}.${member.name}`.toLowerCase(), [member.signature]]]
          : [],
      ),
    ),
  );
}

function objectCompletions(
  catalog: Record<string, BuiltinObjectSpec>,
): Record<string, CompletionItem[]> {
  return Object.fromEntries(
    Object.entries(catalog).map(([objectName, objectSpec]) => [
      objectName,
      objectSpec.members.map(memberCompletionItem),
    ]),
  );
}

function memberCompletionItem(member: BuiltinMemberSpec): CompletionItem {
  return withBuiltinCompletionLabel(
    {
      label: member.name,
      kind: completionKindForMember(member.kind),
      detail: member.type ? `${member.kind} As ${member.type}` : member.kind,
    },
    undefined,
  );
}

function completionKindForMember(kind: VbBuiltinMemberKind): CompletionItemKind {
  switch (kind) {
    case "event":
      return CompletionItemKind.Event;
    case "field":
      return CompletionItemKind.Field;
    case "method":
      return CompletionItemKind.Method;
    case "property":
      return CompletionItemKind.Property;
  }
}

interface BuiltinFunction {
  label: string;
  signature: string;
  returnType: string;
  parameters: BuiltinParameterSpec[];
  documentation: BuiltinDocumentationSpec;
}

const builtinFunctions: BuiltinFunction[] = (
  [
    ["CStr", "CStr(value)", "String", "Converts a value to String."],
    ["CByte", "CByte(value)", "Number", "Converts a value to Byte."],
    ["CInt", "CInt(value)", "Number", "Converts a value to Integer."],
    ["CLng", "CLng(value)", "Number", "Converts a value to Long."],
    ["CSng", "CSng(value)", "Number", "Converts a value to Single."],
    ["CDbl", "CDbl(value)", "Number", "Converts a value to Double."],
    ["CCur", "CCur(value)", "Currency", "Converts a value to Currency."],
    ["CDec", "CDec(value)", "Decimal", "Converts a value to Decimal."],
    ["CBool", "CBool(value)", "Boolean", "Converts a value to Boolean."],
    ["CDate", "CDate(value)", "Date", "Converts a value to Date."],
    ["CVar", "CVar(value)", "Variant", "Converts a value to Variant."],
    ["CVErr", "CVErr(errorNumber)", "Error", "Converts an error number to an Error subtype."],
    ["Asc", "Asc(string)", "Number", "Returns the ANSI character code for a string."],
    ["Chr", "Chr(charCode)", "String", "Returns the character for an ANSI code."],
    ["Hex", "Hex(number)", "String", "Returns the hexadecimal value of a number."],
    ["Oct", "Oct(number)", "String", "Returns the octal value of a number."],
    ["Array", "Array(values)", "Array", "Creates a Variant array."],
    [
      "Filter",
      "Filter(inputStrings, value, include, compare)",
      "Array",
      "Returns matching entries from a string array.",
    ],
    ["Join", "Join(list, delimiter)", "String", "Joins array entries into a string."],
    [
      "LBound",
      "LBound(array, dimension)",
      "Number",
      "Returns the smallest available subscript for an array dimension.",
    ],
    [
      "Split",
      "Split(expression, delimiter, count, compare)",
      "Array",
      "Splits a string into an array.",
    ],
    [
      "UBound",
      "UBound(array, dimension)",
      "Number",
      "Returns the largest available subscript for an array dimension.",
    ],
    ["LCase", "LCase(value)", "String", "Converts a string to lowercase."],
    ["UCase", "UCase(value)", "String", "Converts a string to uppercase."],
    ["Trim", "Trim(value)", "String", "Removes leading and trailing spaces."],
    ["LTrim", "LTrim(value)", "String", "Removes leading spaces."],
    ["RTrim", "RTrim(value)", "String", "Removes trailing spaces."],
    ["Len", "Len(value)", "Number", "Returns the number of characters in a string."],
    [
      "InStr",
      "InStr(start, string1, string2, compare)",
      "Number",
      "Returns the position of one string within another.",
    ],
    [
      "InStrRev",
      "InStrRev(string1, string2, start, compare)",
      "Number",
      "Returns the position of one string within another from the end.",
    ],
    [
      "Replace",
      "Replace(expression, find, replaceWith, start, count, compare)",
      "String",
      "Returns a string with replacements applied.",
    ],
    ["Left", "Left(value, length)", "String", "Returns the left part of a string."],
    ["Right", "Right(value, length)", "String", "Returns the right part of a string."],
    ["Mid", "Mid(value, start, length)", "String", "Returns part of a string."],
    ["Space", "Space(number)", "String", "Returns a string of spaces."],
    ["StrComp", "StrComp(string1, string2, compare)", "Number", "Compares two strings."],
    ["String", "String(number, character)", "String", "Returns a repeated character string."],
    ["StrReverse", "StrReverse(value)", "String", "Reverses a string."],
    ["Date", "Date()", "Date", "Returns the current system date."],
    ["Now", "Now()", "Date", "Returns the current date and time."],
    ["Time", "Time()", "Date", "Returns the current system time."],
    ["Timer", "Timer()", "Number", "Returns the number of seconds since midnight."],
    [
      "DateAdd",
      "DateAdd(interval, number, date)",
      "Date",
      "Returns a date with an interval added.",
    ],
    [
      "DateDiff",
      "DateDiff(interval, date1, date2, firstDayOfWeek, firstWeekOfYear)",
      "Number",
      "Returns the number of intervals between two dates.",
    ],
    [
      "DatePart",
      "DatePart(interval, date, firstDayOfWeek, firstWeekOfYear)",
      "Number",
      "Returns part of a date.",
    ],
    [
      "DateSerial",
      "DateSerial(year, month, day)",
      "Date",
      "Returns a date from year, month, and day values.",
    ],
    ["DateValue", "DateValue(date)", "Date", "Returns a date value."],
    ["Day", "Day(date)", "Number", "Returns the day of the month."],
    [
      "FormatDateTime",
      "FormatDateTime(date, namedFormat)",
      "String",
      "Formats a date or time expression.",
    ],
    ["Hour", "Hour(time)", "Number", "Returns the hour of the day."],
    ["IsDate", "IsDate(value)", "Boolean", "Returns whether a value can be converted to a date."],
    ["Minute", "Minute(time)", "Number", "Returns the minute of the hour."],
    ["Month", "Month(date)", "Number", "Returns the month of the year."],
    ["MonthName", "MonthName(month, abbreviate)", "String", "Returns the name of a month."],
    ["Second", "Second(time)", "Number", "Returns the second of the minute."],
    [
      "TimeSerial",
      "TimeSerial(hour, minute, second)",
      "Date",
      "Returns a time from hour, minute, and second values.",
    ],
    ["TimeValue", "TimeValue(time)", "Date", "Returns a time value."],
    ["Weekday", "Weekday(date, firstDayOfWeek)", "Number", "Returns the weekday number."],
    [
      "WeekdayName",
      "WeekdayName(weekday, abbreviate, firstDayOfWeek)",
      "String",
      "Returns the name of a weekday.",
    ],
    ["Year", "Year(date)", "Number", "Returns the year."],
    [
      "FormatCurrency",
      "FormatCurrency(expression, digitsAfterDecimal, includeLeadingDigit, useParensForNegativeNumbers, groupDigits)",
      "String",
      "Formats an expression as currency.",
    ],
    [
      "FormatNumber",
      "FormatNumber(expression, digitsAfterDecimal, includeLeadingDigit, useParensForNegativeNumbers, groupDigits)",
      "String",
      "Formats an expression as a number.",
    ],
    [
      "FormatPercent",
      "FormatPercent(expression, digitsAfterDecimal, includeLeadingDigit, useParensForNegativeNumbers, groupDigits)",
      "String",
      "Formats an expression as a percentage.",
    ],
    ["Abs", "Abs(number)", "Number", "Returns the absolute value of a number."],
    ["Atn", "Atn(number)", "Number", "Returns the arctangent of a number."],
    ["Cos", "Cos(number)", "Number", "Returns the cosine of an angle."],
    ["Exp", "Exp(number)", "Number", "Returns e raised to a power."],
    ["Fix", "Fix(number)", "Number", "Returns the integer part of a number."],
    ["Int", "Int(number)", "Number", "Returns the integer part of a number."],
    ["Log", "Log(number)", "Number", "Returns the natural logarithm of a number."],
    ["Rnd", "Rnd(number)", "Number", "Returns a random number."],
    ["Round", "Round(number, decimalPlaces)", "Number", "Rounds a number."],
    ["Sgn", "Sgn(number)", "Number", "Returns the sign of a number."],
    ["Sin", "Sin(number)", "Number", "Returns the sine of an angle."],
    ["Sqr", "Sqr(number)", "Number", "Returns the square root of a number."],
    ["Tan", "Tan(number)", "Number", "Returns the tangent of an angle."],
    ["CreateObject", "CreateObject(progId)", "Object", "Creates an automation object."],
    ["Eval", "Eval(expression)", "Variant", "Evaluates an expression."],
    ["IsArray", "IsArray(value)", "Boolean", "Returns whether a value is an array."],
    ["IsNull", "IsNull(value)", "Boolean", "Returns whether a value is Null."],
    ["IsEmpty", "IsEmpty(value)", "Boolean", "Returns whether a variable is Empty."],
    [
      "IsNumeric",
      "IsNumeric(value)",
      "Boolean",
      "Returns whether a value can be evaluated as a number.",
    ],
    ["IsObject", "IsObject(value)", "Boolean", "Returns whether a value is an automation object."],
    ["RGB", "RGB(red, green, blue)", "Number", "Returns an RGB color value."],
    ["ScriptEngine", "ScriptEngine()", "String", "Returns the script engine name."],
    [
      "ScriptEngineBuildVersion",
      "ScriptEngineBuildVersion()",
      "Number",
      "Returns the script engine build version.",
    ],
    [
      "ScriptEngineMajorVersion",
      "ScriptEngineMajorVersion()",
      "Number",
      "Returns the script engine major version.",
    ],
    [
      "ScriptEngineMinorVersion",
      "ScriptEngineMinorVersion()",
      "Number",
      "Returns the script engine minor version.",
    ],
    ["TypeName", "TypeName(value)", "String", "Returns the subtype name for a variable."],
    ["VarType", "VarType(value)", "Number", "Returns the subtype code for a variable."],
  ] satisfies Array<readonly [string, string, string, string]>
).map(([label, signature, returnType, documentation]) => ({
  label,
  signature,
  returnType,
  parameters: parametersFromSignature(signature).map((parameter) =>
    completeParameterSpec({
      ...parameter,
      documentation:
        builtinFunctionDocumentationOverride(label).parameters?.[parameter.name.toLowerCase()] ??
        genericParameterDocumentation(parameter.name),
    }),
  ),
  documentation: builtinFunctionDocumentation(label, documentation, returnType, signature),
}));

const vbKeywords = new Set([
  "and",
  "as",
  "byref",
  "byval",
  "call",
  "case",
  "class",
  "const",
  "dim",
  "do",
  "each",
  "else",
  "elseif",
  "empty",
  "end",
  "exit",
  "explicit",
  "false",
  "for",
  "function",
  "get",
  "if",
  "in",
  "is",
  "let",
  "loop",
  "me",
  "mod",
  "new",
  "next",
  "not",
  "nothing",
  "null",
  "option",
  "or",
  "preserve",
  "private",
  "property",
  "public",
  "redim",
  "rem",
  "select",
  "set",
  "step",
  "sub",
  "then",
  "to",
  "true",
  "until",
  "wend",
  "while",
  "with",
]);

export function parseVbscriptCst(text: string, sourceText = text, baseOffset = 0): VbCstNode {
  const tokens = tokenizeVbscript(text, baseOffset);
  const document: VbCstNode = {
    kind: "Document",
    start: baseOffset,
    end: baseOffset + text.length,
    contentStart: baseOffset,
    contentEnd: baseOffset + text.length,
    tokens,
    children: [],
  };
  const significant = tokens.filter(
    (token) => token.kind !== "whitespace" && token.kind !== "comment",
  );
  const stack: VbCstNode[] = [document];
  for (let index = 0; index < significant.length; index += 1) {
    const token = significant[index];
    if (!isStatementStart(significant, index)) {
      continue;
    }
    const first = lowerToken(token);
    const second = lowerToken(significant[index + 1]);
    if (first === "class" && significant[index + 1]?.kind === "identifier") {
      const node = createBlockNode("Class", token, significant[index + 1], stack);
      addChild(stack.at(-1) ?? document, node);
      stack.push(node);
      continue;
    }
    if (first === "end") {
      closeBlock(stack, second, token);
      continue;
    }
    const declarationStart =
      first === "public" || first === "private" ? lowerToken(significant[index + 1]) : first;
    const declarationOffset = first === "public" || first === "private" ? 1 : 0;
    const visibility =
      first === "public" || first === "private" ? (first as "public" | "private") : undefined;
    if (declarationStart === "sub" || declarationStart === "function") {
      const nameToken = significant[index + declarationOffset + 1];
      if (nameToken?.kind === "identifier") {
        const node = createProcedureNode(
          declarationStart,
          token,
          nameToken,
          collectParameterMetadata(significant, index + declarationOffset + 2),
          stack,
          undefined,
          visibility,
        );
        addChild(stack.at(-1) ?? document, node);
        stack.push(node);
      }
      continue;
    }
    if (declarationStart === "property") {
      const accessor = lowerToken(significant[index + declarationOffset + 1]);
      const nameToken = significant[index + declarationOffset + 2];
      if (
        (accessor === "get" || accessor === "let" || accessor === "set") &&
        nameToken?.kind === "identifier"
      ) {
        const node = createProcedureNode(
          "property",
          token,
          nameToken,
          collectParameterMetadata(significant, index + declarationOffset + 3),
          stack,
          accessor,
          visibility,
        );
        addChild(stack.at(-1) ?? document, node);
        stack.push(node);
      }
      continue;
    }
    const current = stack.at(-1) ?? document;
    if (first === "loop") {
      closeBlock(stack, "loop", token);
      continue;
    }
    if (first === "wend") {
      closeBlock(stack, "wend", token);
      continue;
    }
    if (first === "next") {
      closeBlock(stack, "next", token);
      continue;
    }
    if (first === "if") {
      const node = createStatementNode("If", token, significant, index);
      addChild(current, node);
      if (isMultilineIf(significant, index)) {
        stack.push(node);
      }
      continue;
    }
    if (first === "select" && second === "case") {
      const node = createStatementNode("Select", token, significant, index);
      addChild(current, node);
      stack.push(node);
      continue;
    }
    if (first === "do") {
      const node = createStatementNode("DoLoop", token, significant, index);
      addChild(current, node);
      stack.push(node);
      continue;
    }
    if (first === "while") {
      const node = createStatementNode("While", token, significant, index);
      addChild(current, node);
      stack.push(node);
      continue;
    }
    if (first === "dim" || first === "redim") {
      addChild(
        current,
        createDeclarationNode(token, "VariableDeclaration", first, significant, index + 1),
      );
      continue;
    }
    if (
      (first === "public" || first === "private") &&
      !["sub", "function", "property"].includes(second ?? "")
    ) {
      addChild(
        current,
        createDeclarationNode(
          token,
          "VariableDeclaration",
          first,
          significant,
          index + 1,
          visibility,
        ),
      );
      continue;
    }
    if (first === "const") {
      addChild(
        current,
        createDeclarationNode(token, "ConstantDeclaration", "const", significant, index + 1),
      );
      continue;
    }
    if (first === "for" && second === "each" && significant[index + 2]?.kind === "identifier") {
      const nameToken = significant[index + 2];
      const node: VbCstNode = {
        kind: "ForEach",
        start: token.start,
        end: statementEnd(significant, index),
        nameToken,
        tokens: statementTokens(significant, index),
        children: [],
        declarationKind: "forEach",
        identifiers: [nameToken],
        memberOf: current.kind === "Class" ? current.nameToken?.text : current.memberOf,
        scopeName:
          current.kind === "Procedure" || current.kind === "Property"
            ? current.nameToken?.text
            : undefined,
        scopeStart: token.start,
        scopeEnd: statementEnd(significant, index),
      };
      addChild(current, node);
      stack.push(node);
      continue;
    }
    if (first === "with" && significant[index + 1]?.kind === "identifier") {
      const nameToken = significant[index + 1];
      const node: VbCstNode = {
        kind: "With",
        start: token.start,
        end: statementEnd(significant, index),
        nameToken,
        tokens: statementTokens(significant, index),
        children: [],
        scopeStart: token.start,
        scopeEnd: sourceText.length + baseOffset,
      };
      addChild(current, node);
      stack.push(node);
      continue;
    }
    if (
      first === "set" &&
      significant[index + 1]?.kind === "identifier" &&
      significant[index + 2]?.text === "="
    ) {
      const variableToken = significant[index + 1];
      const newIndex = findKeyword(
        significant,
        index + 3,
        statementEndIndex(significant, index),
        "new",
      );
      const createObjectIndex = findCreateObjectCall(
        significant,
        index + 3,
        statementEndIndex(significant, index),
      );
      if (newIndex !== -1 && significant[newIndex + 1]?.kind === "identifier") {
        addChild(current, {
          kind: "SetNew",
          start: token.start,
          end: statementEnd(significant, index),
          nameToken: variableToken,
          tokens: statementTokens(significant, index),
          children: [],
          typeName: significant[newIndex + 1].text,
        });
      } else if (createObjectIndex !== -1) {
        const stringToken = significant
          .slice(createObjectIndex)
          .find((item) => item.kind === "string");
        if (stringToken) {
          addChild(current, {
            kind: "CreateObject",
            start: token.start,
            end: statementEnd(significant, index),
            nameToken: variableToken,
            tokens: statementTokens(significant, index),
            children: [],
            typeName: stringToken.value ?? unquoteVbString(stringToken.text),
          });
        }
      }
      continue;
    }
    if (first === "call") {
      const nameToken = significant
        .slice(index + 1, statementEndIndex(significant, index))
        .find((item) => item.kind === "identifier");
      addChild(current, createStatementNode("Call", token, significant, index, nameToken));
      continue;
    }
    if (token.kind === "identifier" && statementHasSymbol(significant, index, "=")) {
      addChild(current, createStatementNode("Assignment", token, significant, index, token));
      continue;
    }
    addChild(current, createStatementNode("Expression", token, significant, index));
  }
  closeUnclosedBlocks(stack, document.end);
  return document;
}

function tokenizeVbscript(text: string, baseOffset: number): VbToken[] {
  const tokens: VbToken[] = [];
  let index = 0;
  while (index < text.length) {
    const start = index;
    const char = text[index];
    if (char === "\r" || char === "\n") {
      if (char === "\r" && text[index + 1] === "\n") {
        index += 2;
      } else {
        index += 1;
      }
      tokens.push(token("newline", text, start, index, baseOffset));
      continue;
    }
    if (char === " " || char === "\t") {
      while (index < text.length && (text[index] === " " || text[index] === "\t")) {
        index += 1;
      }
      tokens.push(token("whitespace", text, start, index, baseOffset));
      continue;
    }
    if (char === "'") {
      while (index < text.length && text[index] !== "\r" && text[index] !== "\n") {
        index += 1;
      }
      tokens.push(token("comment", text, start, index, baseOffset));
      continue;
    }
    if (isRemCommentStart(text, index)) {
      index += 3;
      while (index < text.length && text[index] !== "\r" && text[index] !== "\n") {
        index += 1;
      }
      tokens.push(token("comment", text, start, index, baseOffset));
      continue;
    }
    if (char === '"') {
      index += 1;
      while (index < text.length) {
        if (text[index] === '"' && text[index + 1] === '"') {
          index += 2;
          continue;
        }
        if (text[index] === '"') {
          index += 1;
          break;
        }
        index += 1;
      }
      const result = token("string", text, start, index, baseOffset);
      result.value = unquoteVbString(result.text);
      tokens.push(result);
      continue;
    }
    if (isIdentifierStart(char)) {
      index += 1;
      while (index < text.length && isIdentifierPart(text[index])) {
        index += 1;
      }
      const result = token("identifier", text, start, index, baseOffset);
      if (vbKeywords.has(result.text.toLowerCase())) {
        result.kind = "keyword";
      }
      tokens.push(result);
      continue;
    }
    if (/[0-9]/.test(char)) {
      index += 1;
      while (index < text.length && /[0-9.]/.test(text[index])) {
        index += 1;
      }
      tokens.push(token("number", text, start, index, baseOffset));
      continue;
    }
    index += 1;
    tokens.push(token("symbol", text, start, index, baseOffset));
  }
  return tokens;
}

function token(
  kind: VbToken["kind"],
  text: string,
  start: number,
  end: number,
  baseOffset: number,
): VbToken {
  return { kind, start: baseOffset + start, end: baseOffset + end, text: text.slice(start, end) };
}

function isIdentifierStart(char: string): boolean {
  return /[A-Za-z]/.test(char);
}

function isIdentifierPart(char: string): boolean {
  return /[A-Za-z0-9_]/.test(char);
}

function isRemCommentStart(text: string, index: number): boolean {
  if (text.slice(index, index + 3).toLowerCase() !== "rem") {
    return false;
  }
  const after = text[index + 3];
  if (after && isIdentifierPart(after)) {
    return false;
  }
  let cursor = index - 1;
  while (cursor >= 0 && (text[cursor] === " " || text[cursor] === "\t")) {
    cursor -= 1;
  }
  return cursor < 0 || text[cursor] === "\n" || text[cursor] === "\r" || text[cursor] === ":";
}

function isTriviaToken(token: VbToken): boolean {
  return token.kind === "whitespace" || token.kind === "comment" || token.kind === "newline";
}

function isWhitespaceOrNewline(token: VbToken | undefined): boolean {
  return token?.kind === "whitespace" || token?.kind === "newline";
}

function isDocCommentToken(token: VbToken | undefined): token is VbToken {
  return token?.kind === "comment" && token.text.startsWith("'''");
}

function lowerToken(token: VbToken | undefined): string | undefined {
  return token?.text.toLowerCase();
}

function isStatementStart(tokens: VbToken[], index: number): boolean {
  const previous = tokens[index - 1];
  return !previous || previous.kind === "newline" || previous.text === ":";
}

function createBlockNode(
  kind: "Class",
  startToken: VbToken,
  nameToken: VbToken,
  stack: VbCstNode[],
): VbCstNode {
  const parent = stack.at(-1);
  return {
    kind,
    start: startToken.start,
    end: startToken.end,
    nameToken,
    tokens: [startToken, nameToken],
    children: [],
    memberOf: parent?.kind === "Class" ? parent.nameToken?.text : parent?.memberOf,
    scopeStart: startToken.start,
    scopeEnd: startToken.end,
  };
}

function createProcedureNode(
  procedureKind: "sub" | "function" | "property",
  startToken: VbToken,
  nameToken: VbToken,
  parameterMetadata: VbParameterMetadata[],
  stack: VbCstNode[],
  propertyAccessor?: "get" | "let" | "set",
  visibility?: "public" | "private",
): VbCstNode {
  const parentClass = [...stack].reverse().find((node) => node.kind === "Class")?.nameToken?.text;
  return {
    kind: procedureKind === "property" ? "Property" : "Procedure",
    start: startToken.start,
    end: startToken.end,
    nameToken,
    tokens: [startToken, nameToken],
    children: [],
    procedureKind,
    propertyAccessor,
    visibility,
    parameters: parameterMetadata.map((parameter) => parameter.token),
    parameterMetadata,
    memberOf: parentClass,
    scopeName: nameToken.text,
    scopeStart: startToken.start,
    scopeEnd: startToken.end,
  };
}

function addChild(parent: VbCstNode, child: VbCstNode): void {
  parent.children.push(child);
}

function closeBlock(stack: VbCstNode[], endKind: string | undefined, endToken: VbToken): void {
  const targetKind =
    endKind === "class"
      ? "Class"
      : endKind === "property"
        ? "Property"
        : endKind === "with"
          ? "With"
          : endKind === "if"
            ? "If"
            : endKind === "select"
              ? "Select"
              : endKind === "loop"
                ? "DoLoop"
                : endKind === "wend"
                  ? "While"
                  : endKind === "next"
                    ? "ForEach"
                    : "Procedure";
  const index = findLastIndex(stack, (node) => node.kind === targetKind);
  if (index <= 0) {
    return;
  }
  const [node] = stack.splice(index, 1);
  node.end = endToken.end;
  node.scopeEnd = endToken.end;
}

function closeUnclosedBlocks(stack: VbCstNode[], end: number): void {
  for (const node of stack) {
    node.end = Math.max(node.end, end);
    node.scopeEnd = Math.max(node.scopeEnd ?? node.end, end);
  }
}

function collectParameterMetadata(tokens: VbToken[], index: number): VbParameterMetadata[] {
  const parameters: VbParameterMetadata[] = [];
  if (tokens[index]?.text !== "(") {
    return parameters;
  }
  let cursor = index + 1;
  let mode: VbParameterMode | undefined;
  let modeExplicit = false;
  let optional = false;
  let canReadName = true;
  while (cursor < tokens.length && tokens[cursor].text !== ")") {
    const token = tokens[cursor];
    const lower = token.text.toLowerCase();
    if (token.text === ",") {
      mode = undefined;
      modeExplicit = false;
      optional = false;
      canReadName = true;
    } else if (lower === "optional") {
      optional = true;
    } else if (lower === "byval") {
      mode = "byval";
      modeExplicit = true;
    } else if (lower === "byref") {
      mode = "byref";
      modeExplicit = true;
    } else if (canReadName && token.kind === "identifier") {
      parameters.push({ token, mode: mode ?? "byref", modeExplicit, optional });
      canReadName = false;
    }
    cursor += 1;
  }
  return parameters;
}

function createDeclarationNode(
  startToken: VbToken,
  kind: "VariableDeclaration" | "ConstantDeclaration",
  declarationKind: NonNullable<VbCstNode["declarationKind"]>,
  tokens: VbToken[],
  startIndex: number,
  visibility?: "public" | "private",
): VbCstNode {
  const endIndex = statementEndIndex(tokens, startIndex - 1);
  const identifiers: VbToken[] = [];
  const arrayDeclarations: VbArrayDeclaration[] = [];
  let canReadIdentifier = true;
  for (let index = startIndex; index <= endIndex; index += 1) {
    const current = tokens[index];
    if (!current) {
      continue;
    }
    if (current.text === "(") {
      canReadIdentifier = false;
      continue;
    }
    if (current.text === ")" || current.text === ",") {
      canReadIdentifier = current.text === ",";
      continue;
    }
    if (current.text === "=") {
      break;
    }
    if (current.kind === "identifier" && canReadIdentifier) {
      identifiers.push(current);
      const array = readArrayDeclaration(tokens, index, endIndex, declarationKind);
      if (array) {
        arrayDeclarations.push(array);
      }
      canReadIdentifier = false;
    }
  }
  return {
    kind,
    start: startToken.start,
    end: statementEnd(tokens, startIndex - 1),
    tokens: statementTokens(tokens, startIndex - 1),
    children: [],
    declarationKind,
    visibility,
    identifiers,
    arrayDeclarations,
  };
}

function readArrayDeclaration(
  tokens: VbToken[],
  identifierIndex: number,
  endIndex: number,
  declarationKind: NonNullable<VbCstNode["declarationKind"]>,
): VbArrayDeclaration | undefined {
  let openIndex = identifierIndex + 1;
  while (openIndex <= endIndex && isWhitespaceOrNewline(tokens[openIndex])) {
    openIndex += 1;
  }
  if (tokens[openIndex]?.text !== "(") {
    return undefined;
  }
  let depth = 0;
  let closeIndex = -1;
  for (let index = openIndex; index <= endIndex; index += 1) {
    if (tokens[index]?.text === "(") {
      depth += 1;
    } else if (tokens[index]?.text === ")") {
      depth -= 1;
      if (depth === 0) {
        closeIndex = index;
        break;
      }
    }
  }
  if (closeIndex === -1) {
    return undefined;
  }
  const dimensions = arrayDimensionTexts(tokens.slice(openIndex + 1, closeIndex));
  return {
    name: tokens[identifierIndex],
    kind: declarationKind === "redim" || dimensions.length === 0 ? "dynamic" : "fixed",
    dimensions,
  };
}

function arrayDimensionTexts(tokens: VbToken[]): string[] {
  const dimensions: string[] = [];
  let current: VbToken[] = [];
  let depth = 0;
  const flush = (): void => {
    const text = current
      .filter((token) => !isWhitespaceOrNewline(token))
      .map((token) => token.text)
      .join("")
      .trim();
    if (text) {
      dimensions.push(text);
    }
    current = [];
  };
  for (const token of tokens) {
    if (token.text === "(") {
      depth += 1;
    } else if (token.text === ")") {
      depth = Math.max(0, depth - 1);
    }
    if (token.text === "," && depth === 0) {
      flush();
      continue;
    }
    current.push(token);
  }
  flush();
  return dimensions;
}

function createStatementNode(
  kind: "If" | "Select" | "DoLoop" | "While" | "Call" | "Assignment" | "Expression",
  startToken: VbToken,
  tokens: VbToken[],
  startIndex: number,
  nameToken?: VbToken,
): VbCstNode {
  return {
    kind,
    start: startToken.start,
    end: statementEnd(tokens, startIndex),
    nameToken,
    tokens: statementTokens(tokens, startIndex),
    children: [],
    scopeStart: startToken.start,
    scopeEnd: statementEnd(tokens, startIndex),
  };
}

function statementEndIndex(tokens: VbToken[], startIndex: number): number {
  let index = startIndex;
  while (index + 1 < tokens.length) {
    const next = tokens[index + 1];
    if ((next.kind === "newline" && tokens[index]?.text !== "_") || next.text === ":") {
      break;
    }
    index += 1;
  }
  return index;
}

function statementEnd(tokens: VbToken[], startIndex: number): number {
  return tokens[statementEndIndex(tokens, startIndex)]?.end ?? tokens[startIndex]?.end ?? 0;
}

function statementTokens(tokens: VbToken[], startIndex: number): VbToken[] {
  return tokens.slice(startIndex, statementEndIndex(tokens, startIndex) + 1);
}

function isMultilineIf(tokens: VbToken[], startIndex: number): boolean {
  const endIndex = statementEndIndex(tokens, startIndex);
  const thenIndex = findKeyword(tokens, startIndex, endIndex, "then");
  return thenIndex !== -1 && thenIndex === endIndex;
}

function statementHasSymbol(tokens: VbToken[], startIndex: number, symbol: string): boolean {
  const endIndex = statementEndIndex(tokens, startIndex);
  for (let index = startIndex; index <= endIndex; index += 1) {
    if (tokens[index]?.text === symbol) {
      return true;
    }
  }
  return false;
}

function findKeyword(tokens: VbToken[], start: number, end: number, keyword: string): number {
  for (let index = start; index <= end; index += 1) {
    if (lowerToken(tokens[index]) === keyword) {
      return index;
    }
  }
  return -1;
}

function findCreateObjectCall(tokens: VbToken[], start: number, end: number): number {
  for (let index = start; index <= end; index += 1) {
    if (lowerToken(tokens[index]) === "createobject") {
      return index;
    }
    if (
      index + 2 <= end &&
      lowerToken(tokens[index]) === "server" &&
      tokens[index + 1]?.text === "." &&
      lowerToken(tokens[index + 2]) === "createobject"
    ) {
      return index;
    }
  }
  return -1;
}

function unquoteVbString(value: string): string {
  return value.startsWith('"') && value.endsWith('"')
    ? value.slice(1, -1).replaceAll('""', '"')
    : value;
}

export function getVbscriptCompletions(
  parsed: AspParsedDocument,
  position: Position,
  context: VbProjectContext = {},
): CompletionItem[] {
  const sourceOffset = offsetAt(parsed.text, position);
  const symbols = context.symbols ?? collectVbscriptSymbols(parsed, context);
  const docCompletions = getVbDocCommentCompletions(parsed, sourceOffset, symbols, context.locale);
  if (docCompletions.length > 0) {
    return docCompletions;
  }
  const typeEnvironment =
    context.typeEnvironment ?? buildVbTypeEnvironment(parsed, { ...context, symbols });
  const memberTarget = memberCompletionTargetAt(parsed, sourceOffset);
  if (memberTarget) {
    const ownerName = memberTarget.ownerName;
    const builtin = ownerName ? memberCompletions[ownerName.toLowerCase()] : undefined;
    if (builtin) {
      return builtin;
    }
    const ownerType =
      ownerName === undefined
        ? currentWithTypeRef(parsed, sourceOffset, symbols)
        : ownerName.toLowerCase() === "me"
          ? currentClassTypeRef(parsed, sourceOffset, symbols)
          : inferVariableTypeRef(ownerName, parsed, sourceOffset, symbols);
    return ownerType ? typeMemberCompletions(ownerType, symbols, typeEnvironment) : [];
  }
  return dedupeCompletions([
    ...(context.syntaxSnippets === false ? [] : vbscriptSyntaxSnippetCompletions(context.locale)),
    ...builtinCompletions(context.locale),
    ...visibleSymbols(parsed, sourceOffset, symbols).map((symbol) =>
      symbolToCompletion(symbol, context.locale),
    ),
  ]);
}

function memberCompletionTargetAt(
  parsed: AspParsedDocument,
  offset: number,
): { ownerName?: string } | undefined {
  const current = identifierTokenAt(parsed, offset);
  if (current) {
    const dot = previousSignificantToken(parsed, current.start);
    if (dot?.text === ".") {
      return memberCompletionTargetFromDot(parsed, dot);
    }
  }
  const previous = previousSignificantToken(parsed, offset);
  return previous?.text === "." ? memberCompletionTargetFromDot(parsed, previous) : undefined;
}

function memberCompletionTargetFromDot(
  parsed: AspParsedDocument,
  dot: VbToken,
): { ownerName?: string } {
  const owner = previousSignificantToken(parsed, dot.start);
  return owner?.kind === "identifier" ? { ownerName: owner.text } : {};
}

function getVbDocCommentCompletions(
  parsed: AspParsedDocument,
  offset: number,
  symbols: VbSymbol[],
  locale: AspLocale | undefined,
): CompletionItem[] {
  const localizer = createLocalizer(locale);
  const prefix = docCommentLinePrefixAt(parsed.text, offset);
  if (prefix === undefined) {
    return [];
  }
  const paramValue = /<param\b[^>]*\bname\s*=\s*"([^"]*)$/i.exec(prefix);
  if (paramValue) {
    return nextDocumentedProcedureParameters(parsed, offset).map((name) => ({
      label: name,
      kind: CompletionItemKind.Variable,
      detail: localizer.t("vb.doc.detail.parameter"),
    }));
  }
  const crefValue = /\bcref\s*=\s*"([^"]*)$/i.exec(prefix);
  if (crefValue) {
    return dedupeCompletions(
      symbols
        .filter((symbol) => symbol.kind !== "parameter")
        .map((symbol) => ({
          label: symbol.memberOf ? `${symbol.memberOf}.${symbol.name}` : symbol.name,
          kind: symbolToCompletion(symbol, locale).kind,
          detail: localizer.t("vb.doc.detail.cref"),
        })),
    );
  }
  const attribute = /<([A-Za-z][A-Za-z0-9]*)\b([^<>]*)\s+[A-Za-z0-9_-]*$/i.exec(prefix);
  if (attribute && !isInsideXmlAttributeValue(attribute[2] ?? "")) {
    const tag = attribute[1].toLowerCase();
    const used = new Set(
      [...(attribute[2] ?? "").matchAll(/\b([A-Za-z][A-Za-z0-9_-]*)\s*=/g)].map((match) =>
        match[1].toLowerCase(),
      ),
    );
    return (vbDocCommentAttributeCompletions[tag] ?? [])
      .filter((name) => !used.has(name.toLowerCase()))
      .map((name) => ({
        label: name,
        kind: CompletionItemKind.Property,
        detail: localizer.t("vb.doc.detail.attribute"),
        insertText: `${name}="$1"`,
        insertTextFormat: InsertTextFormat.Snippet,
      }));
  }
  const closing = /<\/([A-Za-z0-9_-]*)$/i.exec(prefix);
  if (closing) {
    return unclosedDocCommentTags(parsed, offset).map((tag) => ({
      label: tag,
      kind: CompletionItemKind.Property,
      detail: localizer.t("vb.doc.detail.closingTag"),
      insertText: `${tag}>`,
    }));
  }
  const tag = /<([A-Za-z0-9_-]*)$/i.exec(prefix);
  if (tag) {
    return vbDocCommentTags.map((name) => docCommentTagCompletion(name, locale));
  }
  return [];
}

function docCommentLinePrefixAt(text: string, offset: number): string | undefined {
  const lineStart = text.lastIndexOf("\n", Math.max(0, offset - 1)) + 1;
  const prefix = text.slice(lineStart, offset).replace(/\r$/, "");
  const match = /^\s*'''\s?(.*)$/.exec(prefix);
  return match?.[1];
}

function docCommentTagCompletion(
  tag: (typeof vbDocCommentTags)[number],
  locale: AspLocale | undefined,
): CompletionItem {
  const snippet =
    tag === "see" || tag === "seealso"
      ? `${tag} cref="$1" />`
      : tag === "param"
        ? 'param name="$1">$0</param>'
        : tag === "exception"
          ? 'exception cref="$1">$0</exception>'
          : tag === "list"
            ? 'list type="$1">$0</list>'
            : `${tag}>$0</${tag}>`;
  return {
    label: tag,
    kind: CompletionItemKind.Property,
    detail: createLocalizer(locale).t("vb.doc.detail.tag"),
    insertText: snippet,
    insertTextFormat: InsertTextFormat.Snippet,
  };
}

function isInsideXmlAttributeValue(text: string): boolean {
  return (text.match(/"/g)?.length ?? 0) % 2 === 1 || (text.match(/'/g)?.length ?? 0) % 2 === 1;
}

function nextDocumentedProcedureParameters(parsed: AspParsedDocument, offset: number): string[] {
  const node = vbDocuments(parsed)
    .flatMap((document) => flattenVbNodes(document))
    .filter(
      (candidate) =>
        candidate.start >= offset &&
        (candidate.kind === "Procedure" || candidate.kind === "Property"),
    )
    .sort((left, right) => left.start - right.start)[0];
  return node?.parameters?.map((token) => token.text) ?? [];
}

function unclosedDocCommentTags(parsed: AspParsedDocument, offset: number): string[] {
  const text = docCommentTextUpToOffset(parsed, offset);
  const stack: string[] = [];
  for (const token of tokenizeDocTags(text)) {
    const tag = token.name.toLowerCase();
    if (token.kind === "end") {
      const index = stack.lastIndexOf(tag);
      if (index !== -1) {
        stack.splice(index, 1);
      }
    } else if (
      !token.selfClosing &&
      vbDocCommentTags.includes(tag as (typeof vbDocCommentTags)[number])
    ) {
      stack.push(tag);
    }
  }
  return [...new Set(stack.reverse())];
}

function docCommentTextUpToOffset(parsed: AspParsedDocument, offset: number): string {
  return docCommentBlockAtOffset(parsed, offset)
    .filter((token) => token.start <= offset)
    .map((token) =>
      stripDocCommentPrefix(
        token.start <= offset && offset <= token.end
          ? token.text.slice(0, offset - token.start)
          : token.text,
      ),
    )
    .join("\n");
}

function docCommentBlockAtOffset(parsed: AspParsedDocument, offset: number): VbToken[] {
  const document = vbDocuments(parsed).find(
    (candidate) => offset >= candidate.start && offset <= candidate.end,
  );
  const tokens = document?.tokens ?? [];
  const currentIndex = tokens.findIndex((token) => offset >= token.start && offset <= token.end);
  if (currentIndex === -1 || !isDocCommentToken(tokens[currentIndex])) {
    return [];
  }
  const start = docCommentBlockBoundary(tokens, currentIndex, -1);
  const end = docCommentBlockBoundary(tokens, currentIndex, 1);
  return tokens.slice(start, end + 1).filter(isDocCommentToken);
}

function docCommentBlockBoundary(tokens: VbToken[], startIndex: number, direction: -1 | 1): number {
  let boundary = startIndex;
  let index = startIndex + direction;
  while (index >= 0 && index < tokens.length) {
    while (index >= 0 && index < tokens.length && isWhitespaceOrNewline(tokens[index])) {
      index += direction;
    }
    if (!isDocCommentToken(tokens[index])) {
      break;
    }
    boundary = index;
    index += direction;
  }
  return boundary;
}

export function analyzeVbscript(
  parsed: AspParsedDocument,
  context: VbProjectContext = {},
): { diagnostics: Diagnostic[]; symbols: VbSymbol[] } {
  const symbols = context.symbols ?? collectVbscriptSymbols(parsed, context);
  const diagnostics: Diagnostic[] = [];
  diagnostics.push(...diagnoseDeclarationSyntax(parsed, context.locale));
  diagnostics.push(...diagnoseCallSyntax(parsed, symbols, context.locale));
  const scriptText = getServerScriptText(parsed);
  if (/^\s*Option\s+Explicit\b/im.test(scriptText)) {
    diagnostics.push(...diagnoseUndeclaredVariables(parsed, symbols, context.locale));
  }
  if (context.unusedDiagnostics !== false) {
    diagnostics.push(...diagnoseUnusedSymbols(parsed, symbols, context));
  }
  diagnostics.push(...diagnoseIdentifierCase(parsed, symbols, context));
  if (context.typeChecking === "strict") {
    diagnostics.push(
      ...diagnoseTypeIssues(
        parsed,
        symbols,
        context.typeEnvironment ?? buildVbTypeEnvironment(parsed, { ...context, symbols }),
        context.locale,
      ),
    );
  }
  return { diagnostics: dedupeDiagnostics(diagnostics), symbols };
}

function dedupeDiagnostics(diagnostics: Diagnostic[]): Diagnostic[] {
  const seen = new Set<string>();
  return diagnostics.filter((diagnostic) => {
    const key = diagnosticKey(diagnostic);
    if (seen.has(key)) {
      return false;
    }
    seen.add(key);
    return true;
  });
}

function diagnosticKey(diagnostic: Diagnostic): string {
  return JSON.stringify({
    source: diagnostic.source ?? "",
    code: diagnostic.code ?? "",
    severity: diagnostic.severity ?? "",
    range: diagnostic.range,
    message: diagnostic.message,
  });
}

export function getVbscriptDocumentSymbols(parsed: AspParsedDocument): DocumentSymbol[] {
  return collectVbscriptSymbols(parsed)
    .filter(
      (symbol) =>
        symbol.sourceUri === parsed.uri &&
        (symbol.kind === "function" ||
          symbol.kind === "sub" ||
          symbol.kind === "class" ||
          symbol.kind === "method" ||
          symbol.kind === "property"),
    )
    .map((symbol) => ({
      name: symbol.memberOf ? `${symbol.memberOf}.${symbol.name}` : symbol.name,
      kind:
        symbol.kind === "class"
          ? SymbolKind.Class
          : symbol.kind === "property"
            ? SymbolKind.Property
            : SymbolKind.Function,
      range: symbol.range,
      selectionRange: symbol.range,
    }));
}

export function getVbscriptHover(
  parsed: AspParsedDocument,
  position: Position,
  context: VbProjectContext = {},
): string | undefined {
  const sourceOffset = offsetAt(parsed.text, position);
  const token = identifierTokenAt(parsed, sourceOffset);
  if (!token) {
    return undefined;
  }
  const builtin = builtinDescription(token.text, context.locale);
  if (builtin) {
    return builtin;
  }
  const symbols = context.symbols ?? collectVbscriptSymbols(parsed, context);
  const symbol = resolveSymbolAt(parsed, sourceOffset, symbols);
  if (!symbol) {
    const typeEnvironment =
      context.typeEnvironment ?? buildVbTypeEnvironment(parsed, { ...context, symbols });
    return builtinMemberDescription(parsed, sourceOffset, symbols, typeEnvironment, context.locale);
  }
  const hover = appendDocumentationMarkdown(
    markdownHover(vbscriptHoverSignature(parsed, symbol)),
    symbol.documentation,
    context.locale,
  );
  const typeNote = documentationTypeNoteForDeclarationHover(
    parsed,
    token,
    symbol,
    symbols,
    context.locale,
  );
  return typeNote ? `${hover}\n\n_${typeNote}_` : hover;
}

function markdownHover(signature: string, description?: string): string {
  const base = `\`\`\`vbscript\n${signature}\n\`\`\``;
  return description ? `${base}\n\n${description}` : base;
}

function appendBuiltinDocumentation(
  base: string,
  documentationSpec: BuiltinDocumentationSpec | undefined,
  locale: AspLocale | undefined,
): string {
  const markdown = builtinDocumentationMarkdown(documentationSpec, locale);
  return markdown ? `${base}\n\n${markdown}` : base;
}

function vbscriptHoverSignature(parsed: AspParsedDocument, symbol: VbSymbol): string {
  const type = symbolTypeRef(symbol);
  const typeSuffix = type ? ` As ${formatTypeRef(type)}` : "";
  const arraySuffix = symbolArraySuffix(symbol);
  const visibility = symbol.visibility ? `${titleCaseKeyword(symbol.visibility)} ` : "";
  const parameters = `(${parameterLabels(symbol).join(", ")})`;
  const globalPrefix = isGlobalVariableLikeSymbol(parsed, symbol) ? "(global) " : "";
  if (symbol.kind === "class") {
    return `Class ${symbol.name}`;
  }
  if (symbol.kind === "property") {
    const accessor = titleCaseKeyword(symbol.propertyAccessor ?? "get");
    return `${visibility}Property ${accessor} ${symbol.name}${parameters}${typeSuffix}`;
  }
  if (symbol.kind === "sub") {
    return `${visibility}Sub ${symbol.name}${parameters}`;
  }
  if (symbol.kind === "function") {
    return `${visibility}Function ${symbol.name}${parameters}${typeSuffix}`;
  }
  if (symbol.kind === "method") {
    const keyword = symbol.procedureKind === "sub" ? "Sub" : "Function";
    return keyword === "Sub"
      ? `${visibility}${keyword} ${symbol.name}${parameters}`
      : `${visibility}${keyword} ${symbol.name}${parameters}${typeSuffix}`;
  }
  if (symbol.kind === "field") {
    return `${visibility || "Public "}${symbol.name}${arraySuffix}${typeSuffix}`;
  }
  if (symbol.kind === "constant") {
    return `${globalPrefix}Const ${symbol.name}${typeSuffix}`;
  }
  if (symbol.kind === "parameter") {
    return `${parameterLabel({
      name: symbol.name,
      mode: symbol.parameterMode ?? "byref",
      optional: symbol.optional,
    })}${typeSuffix}`;
  }
  return `${globalPrefix}Dim ${symbol.name}${arraySuffix}${typeSuffix}`;
}

function symbolArraySuffix(symbol: VbSymbol): string {
  if (!symbol.array) {
    return "";
  }
  return `(${symbol.array.dimensions.join(", ")})`;
}

function parameterLabels(symbol: VbSymbol): string[] {
  return parameterDetails(symbol).map(parameterLabel);
}

function parameterDetails(symbol: VbSymbol): VbParameterInfo[] {
  return symbol.parameterDetails && symbol.parameterDetails.length > 0
    ? symbol.parameterDetails
    : (symbol.parameters ?? []).map((name): VbParameterInfo => ({ name, mode: "byref" }));
}

function parameterLabel(parameter: VbParameterInfo): string {
  const optional = parameter.optional ? "Optional " : "";
  return `${optional}${parameterModeKeyword(parameter.mode)} ${parameter.name}`;
}

function parameterModeKeyword(mode: VbParameterMode): "ByRef" | "ByVal" {
  return mode === "byval" ? "ByVal" : "ByRef";
}

function titleCaseKeyword(value: string): string {
  return value.charAt(0).toUpperCase() + value.slice(1).toLowerCase();
}

export function getVbscriptDefinition(
  parsed: AspParsedDocument,
  position: Position,
  context: VbProjectContext = {},
): VbSymbol | undefined {
  return resolveSymbolAt(
    parsed,
    offsetAt(parsed.text, position),
    context.symbols ?? collectVbscriptSymbols(parsed, context),
  );
}

export function getVbscriptReferences(
  parsed: AspParsedDocument,
  position: Position,
  context: VbProjectContext = {},
  options: VbReferenceOptions = {},
): VbReference[] {
  const symbol = getVbscriptDefinition(parsed, position, context);
  if (!symbol) {
    return [];
  }
  const symbols = context.symbols ?? collectVbscriptSymbols(parsed, context);
  const documents = context.documents ?? [parsed];
  const references: VbReference[] = [];
  for (const document of documents) {
    for (const token of identifierTokens(document)) {
      if (token.text.toLowerCase() === symbol.name.toLowerCase()) {
        const resolved = resolveSymbolAt(
          document,
          token.start + Math.floor(token.text.length / 2),
          symbols,
        );
        if (!resolved || !sameSymbol(resolved, symbol)) {
          continue;
        }
        if (options.includeDeclaration === false && isDeclarationNameToken(document, token)) {
          continue;
        }
        if (
          options.includeFunctionReturnAssignments === false &&
          isFunctionReturnAssignmentToken(document, symbol, token)
        ) {
          continue;
        }
        references.push({
          uri: document.uri,
          range: rangeFromOffsets(document.text, token.start, token.end),
        });
      }
    }
  }
  return references;
}

function isFunctionReturnAssignmentToken(
  parsed: AspParsedDocument,
  symbol: VbSymbol,
  token: VbToken,
): boolean {
  if (
    !["function", "method"].includes(symbol.kind) ||
    (symbol.procedureKind && symbol.procedureKind !== "function")
  ) {
    return false;
  }
  const scope = scopeNodeAt(parsed, token.start);
  if (
    scope?.kind !== "Procedure" ||
    scope.procedureKind !== "function" ||
    !scope.nameToken ||
    !sameRange(
      rangeFromOffsets(parsed.text, scope.nameToken.start, scope.nameToken.end),
      symbol.range,
    )
  ) {
    return false;
  }
  const statement = vbStatements(parsed).find((candidate) =>
    candidate.some((item) => item.start === token.start && item.end === token.end),
  );
  if (!statement) {
    return false;
  }
  const targetIndex =
    lowerToken(statement[0]) === "set" || lowerToken(statement[0]) === "let" ? 1 : 0;
  const target = statement[targetIndex];
  return (
    target?.start === token.start &&
    target.end === token.end &&
    statement.some((item, index) => index > targetIndex && item.text === "=")
  );
}

export function getVbscriptSemanticTokens(
  parsed: AspParsedDocument,
  context: VbProjectContext = {},
): VbSemanticToken[] {
  const symbols = context.symbols ?? collectVbscriptSymbols(parsed, context);
  const tokens: VbSemanticToken[] = operatorSemanticTokens(parsed);
  for (const token of identifierTokens(parsed)) {
    if (isClassicAspObjectName(token.text)) {
      tokens.push({
        range: rangeFromOffsets(parsed.text, token.start, token.end),
        tokenType: "variable",
        tokenModifiers: ["library"],
      });
      continue;
    }
    const symbol = resolveSymbolAt(
      parsed,
      token.start + Math.floor(token.text.length / 2),
      symbols,
    );
    if (symbol && !isBuiltinName(symbol.name)) {
      const tokenType = semanticTokenTypeForSymbol(symbol);
      if (!tokenType) {
        continue;
      }
      tokens.push({
        range: rangeFromOffsets(parsed.text, token.start, token.end),
        tokenType,
        tokenModifiers: semanticTokenModifiersForSymbol(symbol),
      });
      continue;
    }
    const builtinToken = builtinSemanticTokenForIdentifier(parsed, token);
    if (builtinToken) {
      tokens.push(builtinToken);
    }
  }
  return tokens;
}

function builtinSemanticTokenForIdentifier(
  parsed: AspParsedDocument,
  token: VbToken,
): VbSemanticToken | undefined {
  const previous = previousSignificantToken(parsed, token.start);
  if (previous?.text === ".") {
    const owner = previousSignificantToken(parsed, previous.start);
    if (
      !owner ||
      !isClassicAspObjectName(owner.text) ||
      !builtinMemberName(owner.text, token.text)
    ) {
      return undefined;
    }
    return {
      range: rangeFromOffsets(parsed.text, token.start, token.end),
      tokenType: builtinSignature(`${owner.text}.${token.text}`) ? "method" : "property",
      tokenModifiers: ["library"],
    };
  }
  if (builtinFunction(token.text)) {
    return {
      range: rangeFromOffsets(parsed.text, token.start, token.end),
      tokenType: "function",
      tokenModifiers: ["library"],
    };
  }
  if (builtinConstant(token.text)) {
    return {
      range: rangeFromOffsets(parsed.text, token.start, token.end),
      tokenType: "variable",
      tokenModifiers: ["readonly", "library"],
    };
  }
  return undefined;
}

function builtinMemberName(owner: string, member: string): boolean {
  return (
    memberCompletions[owner.toLowerCase()]?.some(
      (item) => item.label.toLowerCase() === member.toLowerCase(),
    ) ?? false
  );
}

function operatorSemanticTokens(parsed: AspParsedDocument): VbSemanticToken[] {
  const operators: VbSemanticToken[] = [];
  const documents = vbDocuments(parsed);
  for (const document of documents) {
    const tokens = document.tokens.filter((token) => !isTriviaToken(token));
    for (let index = 0; index < tokens.length; index += 1) {
      const token = tokens[index];
      const next = tokens[index + 1];
      const multiChar =
        token.text === "<" && (next?.text === ">" || next?.text === "=")
          ? next
          : token.text === ">" && next?.text === "="
            ? next
            : undefined;
      if (multiChar) {
        operators.push({
          range: rangeFromOffsets(parsed.text, token.start, multiChar.end),
          tokenType: "operator",
        });
        index += 1;
        continue;
      }
      if (isVbscriptOperator(token.text)) {
        operators.push({
          range: rangeFromOffsets(parsed.text, token.start, token.end),
          tokenType: "operator",
        });
      }
    }
  }
  return operators;
}

function isVbscriptOperator(text: string): boolean {
  return [
    "&",
    "+",
    "-",
    "*",
    "/",
    "\\",
    "^",
    "=",
    "<",
    ">",
    "and",
    "or",
    "not",
    "mod",
    "is",
    "xor",
    "eqv",
    "imp",
  ].includes(text.toLowerCase());
}

function semanticTokenTypeForSymbol(symbol: VbSymbol): VbSemanticToken["tokenType"] | undefined {
  if (symbol.kind === "class") {
    return "class";
  }
  if (symbol.kind === "method") {
    return "method";
  }
  if (symbol.kind === "field" || symbol.kind === "property") {
    return "property";
  }
  if (symbol.kind === "function" || symbol.kind === "sub") {
    return "function";
  }
  if (symbol.kind === "parameter") {
    return "parameter";
  }
  if (symbol.kind === "variable" || symbol.kind === "constant") {
    return "variable";
  }
  return undefined;
}

function semanticTokenModifiersForSymbol(
  symbol: VbSymbol,
): NonNullable<VbSemanticToken["tokenModifiers"]> {
  const modifiers: NonNullable<VbSemanticToken["tokenModifiers"]> = [];
  if (symbol.visibility) {
    modifiers.push(symbol.visibility);
  }
  if (symbol.kind === "constant") {
    modifiers.push("readonly");
  }
  if (symbol.kind === "parameter") {
    modifiers.push(symbol.parameterMode ?? "byref");
  }
  return modifiers;
}

export function getVbscriptRenameRange(
  parsed: AspParsedDocument,
  position: Position,
  context: VbProjectContext = {},
): Range | undefined {
  const offset = offsetAt(parsed.text, position);
  const symbol = resolveSymbolAt(
    parsed,
    offset,
    context.symbols ?? collectVbscriptSymbols(parsed, context),
  );
  if (!symbol || isBuiltinName(symbol.name)) {
    return undefined;
  }
  const token = identifierTokenAt(parsed, offset);
  return token?.text.toLowerCase() === symbol.name.toLowerCase()
    ? rangeFromOffsets(parsed.text, token.start, token.end)
    : undefined;
}

export function getVbscriptDocumentHighlights(
  parsed: AspParsedDocument,
  position: Position,
  context: VbProjectContext = {},
): DocumentHighlight[] {
  return getVbscriptReferences(parsed, position, context)
    .filter((reference) => reference.uri === parsed.uri)
    .map((reference) => ({
      range: reference.range,
      kind: DocumentHighlightKind.Text,
    }));
}

export function getVbscriptSignatureHelp(
  parsed: AspParsedDocument,
  position: Position,
  context: VbProjectContext = {},
): SignatureHelp | undefined {
  const offset = offsetAt(parsed.text, position);
  const call = callExpressionAt(parsed, offset);
  if (!call) {
    return undefined;
  }
  const activeParameter = countActiveParameter(parsed, call.argumentsStart, offset);
  const symbols = context.symbols ?? collectVbscriptSymbols(parsed, context);
  const typeEnvironment =
    context.typeEnvironment ?? buildVbTypeEnvironment(parsed, { ...context, symbols });
  const signatureSymbols = signatureSymbolsForCall(parsed, call.name, offset, symbols);
  if (signatureSymbols.length > 0) {
    return {
      signatures: signatureSymbols.map((symbol) =>
        symbolToSignatureInformation(symbol, context.locale),
      ),
      activeSignature: 0,
      activeParameter,
    };
  }
  const builtinSignatureInfo = builtinSignatureInformationForCall(
    parsed,
    call.name,
    offset,
    symbols,
    typeEnvironment,
    context.locale,
  );
  if (builtinSignatureInfo && builtinSignatureInfo.length > 0) {
    return {
      signatures: builtinSignatureInfo,
      activeSignature: 0,
      activeParameter,
    };
  }
  const signatureLabels =
    typeSignatureLabelsForCall(parsed, call.name, offset, symbols, typeEnvironment) ??
    builtinSignatureLabels(call.name);
  if (!signatureLabels || signatureLabels.length === 0) {
    return undefined;
  }
  return {
    signatures: signatureLabels.map((label) => ({ label })),
    activeSignature: 0,
    activeParameter,
  };
}

export function resolveVbscriptCompletionItem(
  item: CompletionItem,
  parsed: AspParsedDocument,
  context: VbProjectContext = {},
): CompletionItem {
  const symbols = context.symbols ?? collectVbscriptSymbols(parsed, context);
  const env = context.typeEnvironment ?? buildVbTypeEnvironment(parsed, { ...context, symbols });
  const label = item.label.toLowerCase();
  const builtin = builtinCompletions(context.locale).find(
    (candidate) => candidate.label.toLowerCase() === label,
  );
  if (builtin) {
    return {
      ...item,
      detail: item.detail ?? builtin.detail,
      labelDetails: item.labelDetails ?? builtin.labelDetails,
      documentation: item.documentation ?? builtin.documentation,
    };
  }
  const symbol = symbols.find((candidate) => candidate.name.toLowerCase() === label);
  if (symbol) {
    const symbolType = symbolTypeRef(symbol);
    const type = symbolType ? ` As ${formatTypeRef(symbolType)}` : "";
    const owner = symbol.memberOf
      ? createLocalizer(context.locale).t("vb.symbol.owner", { owner: symbol.memberOf })
      : "";
    return {
      ...item,
      detail: `${symbol.kind}${type}${owner}`,
      documentation: appendDocumentationMarkdown(
        `${signatureLabelForDocumentation(symbol)}\n\n${createLocalizer(context.locale).t(
          "vb.completion.definedIn",
          { uri: symbol.sourceUri },
        )}`,
        symbol.documentation,
        context.locale,
      ),
    };
  }
  const member = env.types
    .flatMap((type) => type.members.map((candidate) => ({ type, member: candidate })))
    .find((candidate) => candidate.member.name.toLowerCase() === label);
  if (member) {
    const signature = member.member.signature
      ? signatureLabelFromMember(member.type.name, member.member.name, member.member.signature)
      : undefined;
    const type = member.member.type ? ` As ${formatTypeRef(member.member.type)}` : "";
    const documentationSpec = builtinMemberSpecForType(
      member.type.name,
      member.member.name,
    )?.documentation;
    return {
      ...item,
      detail: signature ?? `${member.member.kind}${type}`,
      documentation:
        builtinDocumentationMarkdown(documentationSpec, context.locale) ??
        createLocalizer(context.locale).t("vb.completion.memberDocumentation", {
          kind: member.member.kind,
          type: member.type.name,
          member: member.member.name,
          suffix: type,
        }),
    };
  }
  return item;
}

export function getVbscriptSelectionRanges(
  parsed: AspParsedDocument,
  positions: Position[],
): SelectionRange[] {
  return positions.map((position) => {
    const offset = offsetAt(parsed.text, position);
    const ranges = uniqueRanges(
      [
        tokenRangeAt(parsed, offset),
        statementRangeAt(parsed, offset),
        ...enclosingVbNodes(parsed, offset).map((node) =>
          rangeFromOffsets(parsed.text, node.start, node.end),
        ),
        regionRangeAt(parsed, offset),
        rangeFromOffsets(parsed.text, 0, parsed.text.length),
      ].filter(isRange),
    );
    return buildSelectionRangeChain(ranges);
  });
}

export function getVbscriptInlayHints(
  parsed: AspParsedDocument,
  range: Range,
  context: VbProjectContext = {},
  options: VbInlayHintOptions = {},
): InlayHint[] {
  const settings = {
    variableTypes: options.variableTypes !== false,
    parameterNames: options.parameterNames !== false,
    functionReturnTypes: options.functionReturnTypes !== false,
    implicitByRef: options.implicitByRef !== false,
  };
  const symbols = context.symbols ?? collectVbscriptSymbols(parsed, context);
  const env = context.typeEnvironment ?? buildVbTypeEnvironment(parsed, { ...context, symbols });
  const hints: InlayHint[] = [];
  const rangeStart = offsetAt(parsed.text, range.start);
  const rangeEnd = offsetAt(parsed.text, range.end);
  if (settings.variableTypes) {
    for (const symbol of symbols) {
      if (
        symbol.sourceUri !== parsed.uri ||
        !["variable", "constant", "field"].includes(symbol.kind) ||
        !symbol.typeName ||
        isHiddenInlayType(symbol.typeName) ||
        !rangeOverlapsOffsets(parsed.text, symbol.range, rangeStart, rangeEnd)
      ) {
        continue;
      }
      hints.push({
        position: symbol.range.end,
        label: `${globalInlayPrefix(parsed, symbol)} As ${symbol.typeName}`,
        kind: InlayHintKind.Type,
        paddingLeft: false,
        paddingRight: true,
        tooltip: "Inferred VBScript type",
      });
    }
  }
  if (settings.functionReturnTypes) {
    for (const symbol of symbols) {
      if (
        symbol.sourceUri !== parsed.uri ||
        !["function", "property"].includes(symbol.kind) ||
        !symbol.typeName ||
        isHiddenInlayType(symbol.typeName) ||
        !rangeOverlapsOffsets(parsed.text, symbol.range, rangeStart, rangeEnd)
      ) {
        continue;
      }
      hints.push({
        position: functionReturnHintPosition(parsed, symbol),
        label: ` As ${symbol.typeName}`,
        kind: InlayHintKind.Type,
        paddingLeft: false,
        paddingRight: true,
        tooltip: "Inferred VBScript return type",
      });
    }
  }
  if (settings.implicitByRef) {
    for (const node of vbProcedureNodes(parsed)) {
      for (const parameter of node.parameterMetadata ?? []) {
        if (
          parameter.modeExplicit ||
          parameter.mode !== "byref" ||
          parameter.token.start < rangeStart ||
          parameter.token.start > rangeEnd
        ) {
          continue;
        }
        hints.push({
          position: rangeFromOffsets(parsed.text, parameter.token.start, parameter.token.end).start,
          label: "ByRef ",
          kind: InlayHintKind.Parameter,
          paddingRight: false,
          tooltip: "Implicit VBScript ByRef parameter",
        });
      }
    }
  }
  if (settings.parameterNames) {
    for (const statement of vbStatements(parsed)) {
      for (let index = 0; index < statement.length; index += 1) {
        if (statement[index].text !== "(" || statement[index - 1]?.kind !== "identifier") {
          continue;
        }
        if (isProcedureDeclarationOpenParen(parsed, statement, index)) {
          continue;
        }
        const name = callNameBefore(statement, index);
        const signature = name
          ? signatureForCall(parsed, name, statement[index].start, symbols, env)
          : undefined;
        if (!signature) {
          continue;
        }
        const closeIndex = matchingCloseParen(statement, index);
        const argumentTokens = topLevelArgumentStarts(
          statement.slice(index + 1, closeIndex === -1 ? undefined : closeIndex),
        );
        for (const [argumentIndex, token] of argumentTokens.entries()) {
          const parameter = signature.parameters[argumentIndex];
          if (
            !parameter ||
            token.start < rangeStart ||
            token.start > rangeEnd ||
            isNamedArgument(statement, token)
          ) {
            continue;
          }
          hints.push({
            position: rangeFromOffsets(parsed.text, token.start, token.end).start,
            label: `${parameter.name}:`,
            kind: InlayHintKind.Parameter,
            paddingRight: true,
            tooltip: "VBScript parameter name",
          });
        }
      }
    }
  }
  return hints.sort(
    (left, right) =>
      left.position.line - right.position.line ||
      left.position.character - right.position.character,
  );
}

function vbProcedureNodes(parsed: AspParsedDocument): VbCstNode[] {
  return vbDocuments(parsed).flatMap((document) =>
    flattenVbNodes(document).filter(
      (node) => node.kind === "Procedure" || node.kind === "Property",
    ),
  );
}

function functionReturnHintPosition(parsed: AspParsedDocument, symbol: VbSymbol): Position {
  const nameEnd = offsetAt(parsed.text, symbol.range.end);
  const closeParenEnd = declarationCloseParenEnd(parsed.text, nameEnd);
  return closeParenEnd
    ? rangeFromOffsets(parsed.text, closeParenEnd, closeParenEnd).start
    : symbol.range.end;
}

function isProcedureDeclarationOpenParen(
  parsed: AspParsedDocument,
  statement: VbToken[],
  openParenIndex: number,
): boolean {
  const nameToken = statement[openParenIndex - 1];
  return Boolean(
    nameToken?.kind === "identifier" &&
    vbProcedureNodes(parsed).some(
      (node) => node.nameToken?.start === nameToken.start && node.nameToken.end === nameToken.end,
    ),
  );
}

function declarationCloseParenEnd(text: string, nameEnd: number): number | undefined {
  const lineEnd = text.slice(nameEnd).search(/\r|\n/);
  const end = lineEnd === -1 ? text.length : nameEnd + lineEnd;
  const open = text.indexOf("(", nameEnd);
  if (open === -1 || open > end) {
    return undefined;
  }
  let depth = 0;
  for (let offset = open; offset < end; offset += 1) {
    if (text[offset] === "(") {
      depth += 1;
    } else if (text[offset] === ")") {
      depth -= 1;
      if (depth === 0) {
        return offset + 1;
      }
    }
  }
  return undefined;
}

function globalInlayPrefix(parsed: AspParsedDocument, symbol: VbSymbol): string {
  return isGlobalVariableLikeSymbol(parsed, symbol) ? " (global)" : "";
}

function isGlobalVariableLikeSymbol(parsed: AspParsedDocument, symbol: VbSymbol): boolean {
  return (
    symbol.sourceUri === parsed.uri &&
    (symbol.kind === "variable" || symbol.kind === "constant") &&
    !symbol.scopeName &&
    !symbol.memberOf
  );
}

function isHiddenInlayType(typeName: string): boolean {
  return typeName.toLowerCase() === "unknown";
}

export function getVbscriptTypeDefinition(
  parsed: AspParsedDocument,
  position: Position,
  context: VbProjectContext = {},
): VbSymbol | undefined {
  const offset = offsetAt(parsed.text, position);
  const symbols = context.symbols ?? collectVbscriptSymbols(parsed, context);
  const symbol = resolveSymbolAt(parsed, offset, symbols);
  const type = symbolTypeRef(symbol) ?? typeRefAtOffset(parsed, offset, symbols);
  const targetType = type ? typeWithoutNothing(type) : undefined;
  if (!targetType || targetType.unionTypes || isLooseType(targetType)) {
    return undefined;
  }
  return symbols.find(
    (candidate) =>
      candidate.kind === "class" && candidate.name.toLowerCase() === targetType.name.toLowerCase(),
  );
}

export function getVbscriptImplementation(
  parsed: AspParsedDocument,
  position: Position,
  context: VbProjectContext = {},
): VbSymbol | undefined {
  const symbol = getVbscriptDefinition(parsed, position, context);
  return symbol && !isBuiltinName(symbol.name) ? symbol : undefined;
}

export function prepareVbscriptCallHierarchy(
  parsed: AspParsedDocument,
  position: Position,
  context: VbProjectContext = {},
  rootUri = parsed.uri,
): CallHierarchyItem[] {
  const symbols = context.symbols ?? collectVbscriptSymbols(parsed, context);
  const symbol = resolveSymbolAt(parsed, offsetAt(parsed.text, position), symbols);
  if (!symbol || !isCallableHierarchySymbol(symbol)) {
    return [];
  }
  return [symbolToCallHierarchyItem(symbol, rootUri)];
}

export function getVbscriptIncomingCalls(
  item: CallHierarchyItem,
  context: VbProjectContext = {},
): CallHierarchyIncomingCall[] {
  const symbols = context.symbols ?? [];
  const documents = context.documents ?? [];
  const target = callHierarchyTargetSymbol(item, symbols);
  if (!target) {
    return [];
  }
  const callers = new Map<string, { item: CallHierarchyItem; ranges: Range[] }>();
  for (const document of documents) {
    for (const call of callSitesInDocument(document)) {
      const resolved = resolveCallTargetSymbol(document, call.name, call.offset, symbols);
      if (!resolved || !sameSymbol(resolved, target)) {
        continue;
      }
      const caller = enclosingCallableSymbol(document, call.offset, symbols);
      if (!caller) {
        continue;
      }
      const key = symbolKey(caller);
      const existing =
        callers.get(key) ??
        ({
          item: symbolToCallHierarchyItem(caller, callHierarchyRootUri(item)),
          ranges: [],
        } satisfies { item: CallHierarchyItem; ranges: Range[] });
      existing.ranges.push(call.range);
      callers.set(key, existing);
    }
  }
  return [...callers.values()].map((caller) => ({
    from: caller.item,
    fromRanges: caller.ranges,
  }));
}

export function getVbscriptOutgoingCalls(
  item: CallHierarchyItem,
  context: VbProjectContext = {},
): CallHierarchyOutgoingCall[] {
  const symbols = context.symbols ?? [];
  const documents = context.documents ?? [];
  const source = callHierarchyTargetSymbol(item, symbols);
  if (!source?.scopeRange) {
    return [];
  }
  const document = documents.find((candidate) => candidate.uri === source.sourceUri);
  if (!document) {
    return [];
  }
  const start = offsetAt(document.text, source.scopeRange.start);
  const end = offsetAt(document.text, source.scopeRange.end);
  const callees = new Map<string, { item: CallHierarchyItem; ranges: Range[] }>();
  for (const call of callSitesInDocument(document).filter(
    (candidate) => candidate.offset >= start && candidate.offset <= end,
  )) {
    const resolved = resolveCallTargetSymbol(document, call.name, call.offset, symbols);
    if (!resolved || sameSymbol(resolved, source) || !isCallableHierarchySymbol(resolved)) {
      continue;
    }
    const key = symbolKey(resolved);
    const existing =
      callees.get(key) ??
      ({
        item: symbolToCallHierarchyItem(resolved, callHierarchyRootUri(item)),
        ranges: [],
      } satisfies { item: CallHierarchyItem; ranges: Range[] });
    existing.ranges.push(call.range);
    callees.set(key, existing);
  }
  return [...callees.values()].map((callee) => ({
    to: callee.item,
    fromRanges: callee.ranges,
  }));
}

export function collectVbscriptSymbols(
  parsed: AspParsedDocument,
  context: VbProjectContext = {},
): VbSymbol[] {
  const symbols: VbSymbol[] = [];
  for (const node of vbDocuments(parsed)) {
    addSymbolsFromVbNode(parsed, node, symbols);
  }
  addImplicitAssignmentSymbols(parsed, symbols);
  applyTypeAnnotations(parsed, symbols);
  inferAssignedTypes(parsed, symbols, context);
  applyTypeAnnotations(parsed, symbols);
  applyVariantFallbackTypes(symbols);
  return symbols;
}

export function buildVbTypeEnvironment(
  parsed: AspParsedDocument,
  context: VbProjectContext = {},
): VbTypeEnvironment {
  const symbols = context.symbols ?? collectVbscriptSymbols(parsed, context);
  const typeMap = new Map<string, VbType>();
  for (const type of builtinTypes()) {
    addType(typeMap, type);
  }
  for (const type of configuredComTypes(context.comTypes ?? {})) {
    addType(typeMap, type);
  }
  for (const symbol of symbols.filter((item) => item.kind === "class")) {
    addType(typeMap, {
      name: symbol.name,
      kind: "class",
      members: symbols
        .filter((member) => member.memberOf?.toLowerCase() === symbol.name.toLowerCase())
        .map(
          (member): VbMember => ({
            name: member.name,
            kind:
              member.kind === "method" ? "method" : member.kind === "field" ? "field" : "property",
            type: member.type ?? typeRef(member.typeName ?? "Variant"),
            signature:
              member.kind === "method" || member.kind === "property"
                ? {
                    parameters: parameterDetails(member).map((parameter) => ({
                      name: parameter.name,
                      mode: parameter.mode,
                      optional: parameter.optional,
                    })),
                    returnType: member.type ?? typeRef(member.typeName ?? "Variant"),
                  }
                : undefined,
          }),
        ),
    });
  }
  for (const annotation of parseTypeAnnotations(parsed).members) {
    const existing = typeMap.get(annotation.typeName.toLowerCase()) ?? {
      name: annotation.typeName,
      kind: "class" as const,
      members: [],
    };
    existing.members = [
      ...existing.members.filter(
        (member) => member.name.toLowerCase() !== annotation.memberName.toLowerCase(),
      ),
      {
        name: annotation.memberName,
        kind: "property",
        type: typeRef(annotation.memberType),
      },
    ];
    addType(typeMap, existing);
  }
  return { types: [...typeMap.values()], symbols };
}

function addSymbolsFromVbNode(
  parsed: AspParsedDocument,
  node: VbCstNode,
  symbols: VbSymbol[],
): void {
  const documentation = documentationForNode(parsed, node);
  if (node.kind === "Class" && node.nameToken) {
    symbols.push({
      name: node.nameToken.text,
      kind: "class",
      range: rangeFromOffsets(parsed.text, node.nameToken.start, node.nameToken.end),
      sourceUri: parsed.uri,
      scopeRange: rangeFromOffsets(parsed.text, node.start, node.end),
      documentation,
    });
  }
  if ((node.kind === "Procedure" || node.kind === "Property") && node.nameToken) {
    const name = node.nameToken.text;
    const kind: VbSymbolKind =
      node.kind === "Property"
        ? "property"
        : node.memberOf
          ? "method"
          : node.procedureKind === "sub"
            ? "sub"
            : "function";
    symbols.push({
      name,
      kind,
      range: rangeFromOffsets(parsed.text, node.nameToken.start, node.nameToken.end),
      sourceUri: parsed.uri,
      memberOf: node.memberOf,
      containerName: node.memberOf,
      scopeName: undefined,
      scopeRange: rangeFromOffsets(parsed.text, node.start, node.end),
      parameters: node.parameters?.map((token) => token.text) ?? [],
      parameterDetails:
        node.parameterMetadata?.map((parameter) => ({
          name: parameter.token.text,
          mode: parameter.mode,
          optional: parameter.optional || undefined,
        })) ?? [],
      visibility: node.visibility,
      procedureKind:
        node.procedureKind === "function" || node.procedureKind === "sub"
          ? node.procedureKind
          : undefined,
      propertyAccessor: node.propertyAccessor,
      documentation,
    });
    for (const parameter of node.parameterMetadata ?? []) {
      symbols.push({
        name: parameter.token.text,
        kind: "parameter",
        range: rangeFromOffsets(parsed.text, parameter.token.start, parameter.token.end),
        sourceUri: parsed.uri,
        scopeName: name,
        scopeRange: rangeFromOffsets(parsed.text, node.start, node.end),
        parameterMode: parameter.mode,
        optional: parameter.optional || undefined,
        documentation: documentationForParameter(documentation, parameter.token.text),
      });
    }
  }
  if (
    node.kind === "VariableDeclaration" ||
    node.kind === "ConstantDeclaration" ||
    node.kind === "ForEach"
  ) {
    const baseKind: "variable" | "constant" =
      node.kind === "ConstantDeclaration" ? "constant" : "variable";
    const scope = scopeNodeAt(parsed, node.start);
    const memberOf = scope ? undefined : (node.memberOf ?? parentClassName(parsed, node.start));
    const identifiers = node.identifiers ?? (node.nameToken ? [node.nameToken] : []);
    const variableDocumentation = identifiers.length === 1 ? documentation : undefined;
    for (const identifier of identifiers) {
      const array = node.arrayDeclarations?.find((item) => item.name === identifier);
      symbols.push({
        name: identifier.text,
        kind: memberOf && baseKind === "variable" ? "field" : baseKind,
        range: rangeFromOffsets(parsed.text, identifier.start, identifier.end),
        sourceUri: parsed.uri,
        memberOf,
        containerName: memberOf,
        scopeName: scope?.nameToken?.text,
        scopeRange: scope
          ? rangeFromOffsets(parsed.text, scope.start, scope.end)
          : memberOf
            ? rangeFromOffsets(parsed.text, node.start, node.end)
            : undefined,
        type: array ? typeRef("Array") : undefined,
        typeName: array ? "Array" : undefined,
        explicitType: Boolean(array),
        array: array
          ? {
              kind: array.kind,
              dimensions: array.dimensions,
            }
          : undefined,
        visibility: node.visibility,
        documentation: variableDocumentation,
      });
    }
  }
  for (const child of node.children) {
    addSymbolsFromVbNode(parsed, child, symbols);
  }
}

function addImplicitAssignmentSymbols(parsed: AspParsedDocument, symbols: VbSymbol[]): void {
  if (hasOptionExplicit(parsed)) {
    return;
  }
  for (const statement of vbStatements(parsed)) {
    const first = lowerToken(statement[0]);
    const targetIndex = first === "set" ? 1 : 0;
    const target = statement[targetIndex];
    const equalsIndex = statement.findIndex((token) => token.text === "=");
    if (
      !target ||
      target.kind !== "identifier" ||
      equalsIndex === -1 ||
      equalsIndex <= targetIndex ||
      statement[targetIndex + 1]?.text === "." ||
      isBuiltinName(target.text) ||
      isImplicitKeywordName(target.text)
    ) {
      continue;
    }
    const scope = scopeNodeAt(parsed, target.start);
    const memberOf = scope ? undefined : parentClassName(parsed, target.start);
    if (scope?.nameToken?.text.toLowerCase() === target.text.toLowerCase()) {
      continue;
    }
    if (
      symbols.some(
        (symbol) =>
          symbol.name.toLowerCase() === target.text.toLowerCase() &&
          symbol.sourceUri === parsed.uri &&
          (symbol.scopeName ?? "") === (scope?.nameToken?.text ?? "") &&
          (symbol.memberOf ?? "") === (memberOf ?? "") &&
          isSymbolVisibleAt(symbol, parsed.uri, parsed.text, target.start),
      )
    ) {
      continue;
    }
    symbols.push({
      name: target.text,
      kind: "variable",
      range: rangeFromOffsets(parsed.text, target.start, target.end),
      sourceUri: parsed.uri,
      memberOf,
      containerName: memberOf,
      scopeName: scope?.nameToken?.text,
      scopeRange: scope
        ? rangeFromOffsets(parsed.text, scope.start, scope.end)
        : memberOf
          ? rangeFromOffsets(parsed.text, target.start, statement.at(-1)?.end ?? target.end)
          : undefined,
      implicit: true,
    });
  }
}

function hasOptionExplicit(parsed: AspParsedDocument): boolean {
  return /^\s*Option\s+Explicit\b/im.test(getServerScriptText(parsed));
}

function isImplicitKeywordName(name: string): boolean {
  return ["true", "false", "nothing", "empty", "null", "me"].includes(name.toLowerCase());
}

function inferAssignedTypes(
  parsed: AspParsedDocument,
  symbols: VbSymbol[],
  context: VbProjectContext,
): void {
  const byName = new Map<string, VbSymbol[]>();
  for (const symbol of symbols) {
    const list = byName.get(symbol.name.toLowerCase()) ?? [];
    list.push(symbol);
    byName.set(symbol.name.toLowerCase(), list);
  }
  for (const document of vbDocuments(parsed)) {
    for (const node of flattenVbNodes(document)) {
      if (
        (node.kind !== "SetNew" && node.kind !== "CreateObject") ||
        !node.nameToken ||
        !node.typeName
      ) {
        continue;
      }
      const candidates = (byName.get(node.nameToken.text.toLowerCase()) ?? []).filter(
        (symbol) => symbol.kind === "variable" || symbol.kind === "field",
      );
      const visible =
        candidates.find((candidate) =>
          isSymbolVisibleAt(candidate, parsed.uri, parsed.text, node.start),
        ) ?? candidates[0];
      if (visible && !visible.explicitType) {
        const assignedType = typeRef(node.typeName);
        setSymbolTypeRef(
          visible,
          mergeTypeRefs(symbolTypeRef(visible), assignedType) ?? assignedType,
        );
      }
    }
  }
  inferStatementTypes(parsed, symbols, context);
}

function inferStatementTypes(
  parsed: AspParsedDocument,
  symbols: VbSymbol[],
  context: VbProjectContext,
): void {
  const env = buildVbTypeEnvironment(parsed, { ...context, symbols });
  for (const statement of vbStatements(parsed)) {
    const first = lowerToken(statement[0]);
    const targetIndex = first === "set" ? 1 : 0;
    const target = statement[targetIndex];
    const equalsIndex = statement.findIndex((token) => token.text === "=");
    if (
      !target ||
      target.kind !== "identifier" ||
      equalsIndex === -1 ||
      statement[targetIndex + 1]?.text === "."
    ) {
      continue;
    }
    const symbol = visibleSymbols(parsed, target.start, symbols).find(
      (item) => item.name.toLowerCase() === target.text.toLowerCase(),
    );
    if (!symbol) {
      continue;
    }
    const expressionType = inferExpressionType(
      parsed,
      statement.slice(equalsIndex + 1),
      symbols,
      env,
      target.start,
    );
    if (!expressionType || symbol.explicitType) {
      continue;
    }
    const existingType = symbolTypeRef(symbol);
    if (!existingType || isLooseType(existingType)) {
      setSymbolTypeRef(symbol, expressionType);
    } else {
      setSymbolTypeRef(symbol, mergeTypeRefs(existingType, expressionType) ?? expressionType);
    }
  }
  inferFunctionReturnTypes(parsed, symbols, env);
}

function inferFunctionReturnTypes(
  parsed: AspParsedDocument,
  symbols: VbSymbol[],
  env: VbTypeEnvironment,
): void {
  for (const node of vbDocuments(parsed).flatMap((document) => flattenVbNodes(document))) {
    if (node.kind !== "Procedure" || node.procedureKind !== "function" || !node.nameToken) {
      continue;
    }
    const symbol = symbols.find(
      (candidate) =>
        candidate.kind === "function" &&
        candidate.name.toLowerCase() === node.nameToken?.text.toLowerCase() &&
        candidate.range.start.line ===
          rangeFromOffsets(parsed.text, node.nameToken.start, node.nameToken.end).start.line,
    );
    if (!symbol || symbol.explicitType) {
      continue;
    }
    const returnType = vbStatements(parsed)
      .filter((statement) => statement[0]?.start >= node.start && statement[0]?.end <= node.end)
      .filter(
        (statement) =>
          statement[0]?.kind === "identifier" &&
          statement[0].text.toLowerCase() === node.nameToken?.text.toLowerCase() &&
          statement[1]?.text === "=",
      )
      .map((statement) =>
        inferExpressionType(parsed, statement.slice(2), symbols, env, statement[0].start),
      )
      .reduce<VbTypeRef | undefined>((merged, type) => mergeTypeRefs(merged, type), undefined);
    if (returnType) {
      setSymbolTypeRef(symbol, returnType);
    }
  }
}

interface TypeAnnotations {
  types: Array<{ name: string; typeName: string; offset: number }>;
  params: Array<{ name: string; typeName: string; procedureName?: string }>;
  returns: Array<{ name: string; typeName: string }>;
  members: Array<{ typeName: string; memberName: string; memberType: string }>;
}

function parseTypeAnnotations(parsed: AspParsedDocument): TypeAnnotations {
  const annotations: TypeAnnotations = { types: [], params: [], returns: [], members: [] };
  for (const document of vbDocuments(parsed)) {
    for (const token of document.tokens.filter((item) => item.kind === "comment")) {
      const text = token.text.replace(/^'\s*/, "").trim();
      const type = /^@type\s+([A-Za-z_][A-Za-z0-9_]*)\s+As\s+(.+)$/i.exec(text);
      if (type) {
        annotations.types.push({ name: type[1], typeName: type[2], offset: token.start });
        continue;
      }
      const param =
        /^@param\s+([A-Za-z_][A-Za-z0-9_]*)(?:\.([A-Za-z_][A-Za-z0-9_]*))?\s+As\s+(.+)$/i.exec(
          text,
        );
      if (param) {
        annotations.params.push({
          procedureName: param[2] ? param[1] : undefined,
          name: param[2] ?? param[1],
          typeName: param[3],
        });
        continue;
      }
      const returns = /^@returns(?:\s+(.+))?$/i.exec(text);
      if (returns) {
        const returnsBody = returns[1]?.trim();
        const [procedureName, returnType] = parseReturnsTypeAnnotation(returnsBody);
        const procedure = procedureName ?? nextProcedureName(parsed, token.start);
        if (procedure) {
          annotations.returns.push({ name: procedure, typeName: returnType });
        }
        continue;
      }
      const member =
        /^@member\s+([A-Za-z_][A-Za-z0-9_.]*)\.([A-Za-z_][A-Za-z0-9_]*)\s+As\s+(.+)$/i.exec(text);
      if (member) {
        annotations.members.push({
          typeName: member[1],
          memberName: member[2],
          memberType: member[3],
        });
      }
    }
  }
  return annotations;
}

function parseReturnsTypeAnnotation(
  body: string | undefined,
): [procedureName: string | undefined, typeName: string] {
  if (!body) {
    return [undefined, "Variant"];
  }
  const firstSpace = body.search(/\s/);
  if (firstSpace === -1) {
    return [undefined, body];
  }
  const rest = body.slice(firstSpace).trim();
  return rest.startsWith("|") ? [undefined, body] : [body.slice(0, firstSpace), rest];
}

function documentationForNode(
  parsed: AspParsedDocument,
  node: VbCstNode,
): VbDocumentation | undefined {
  if (!node.nameToken && !node.identifiers?.length) {
    return undefined;
  }
  const tokens = docCommentBlockBefore(parsed, node.start);
  return tokens.length > 0 ? parseVbDocumentation(tokens) : undefined;
}

function documentationForParameter(
  documentation: VbDocumentation | undefined,
  name: string,
): VbDocumentation | undefined {
  const summary = documentationParameterText(documentation, name);
  return summary
    ? {
        summary,
        params: {},
        exceptions: [],
        see: [],
        seealso: [],
      }
    : undefined;
}

function docCommentBlockBefore(parsed: AspParsedDocument, offset: number): VbToken[] {
  const document = vbDocuments(parsed).find(
    (candidate) => offset >= candidate.start && offset <= candidate.end,
  );
  const tokens = document?.tokens ?? [];
  let index = tokens.findIndex((token) => token.start >= offset);
  index = index === -1 ? tokens.length - 1 : index - 1;
  while (index >= 0 && isWhitespaceOrNewline(tokens[index])) {
    index -= 1;
  }
  const comments: VbToken[] = [];
  while (index >= 0) {
    const current = tokens[index];
    if (!isDocCommentToken(current)) {
      break;
    }
    comments.push(current);
    index -= 1;
    while (index >= 0 && isWhitespaceOrNewline(tokens[index])) {
      index -= 1;
    }
  }
  return comments.reverse();
}

function parseVbDocumentation(tokens: VbToken[]): VbDocumentation | undefined {
  const xmlText = tokens.map((token) => stripDocCommentPrefix(token.text)).join("\n");
  const docRoot = parseVbDocXml(xmlText);
  const documentation: VbDocumentation = {
    params: {},
    exceptions: [],
    see: [],
    seealso: [],
  };
  documentation.summary = firstDocElementText(docRoot, "summary");
  documentation.remarks = firstDocElementText(docRoot, "remarks");
  documentation.returns = firstDocElementText(docRoot, "returns");
  documentation.value = firstDocElementText(docRoot, "value");
  documentation.example = firstDocElementText(docRoot, "example");
  documentation.code = firstDocElementText(docRoot, "code", true);
  for (const element of docElements(docRoot, "param")) {
    const name = element.attributes.name;
    if (name) {
      documentation.params[name] = docElementText(element);
    }
  }
  for (const element of docElements(docRoot, "exception")) {
    documentation.exceptions.push({
      cref: element.attributes.cref,
      text: docElementText(element),
    });
  }
  for (const tag of ["see", "seealso"] as const) {
    for (const element of docElements(docRoot, tag)) {
      documentation[tag].push({
        cref: element.attributes.cref,
        href: element.attributes.href,
        langword: element.attributes.langword,
        text: docElementText(element),
      });
    }
  }
  return hasDocumentationContent(documentation) ? documentation : undefined;
}

function stripDocCommentPrefix(text: string): string {
  return text.replace(/^'''\s?/, "");
}

function parseVbDocXml(text: string): VbDocElement {
  const root: VbDocElement = { name: "__root", attributes: {}, children: [] };
  const stack: VbDocElement[] = [root];
  let cursor = 0;
  for (const token of tokenizeDocXml(text)) {
    if (token.start > cursor) {
      stack.at(-1)?.children.push(text.slice(cursor, token.start));
    }
    cursor = token.end;
    if (token.kind === "text") {
      stack.at(-1)?.children.push(token.text);
      continue;
    }
    if (token.kind === "start") {
      const element: VbDocElement = {
        name: token.name.toLowerCase(),
        attributes: token.attributes,
        children: [],
        selfClosing: token.selfClosing,
      };
      stack.at(-1)?.children.push(element);
      if (!token.selfClosing) {
        stack.push(element);
      }
      continue;
    }
    const index = findLastIndex(stack, (element) => element.name === token.name.toLowerCase());
    if (index > 0) {
      stack.splice(index);
    }
  }
  if (cursor < text.length) {
    stack.at(-1)?.children.push(text.slice(cursor));
  }
  return root;
}

function tokenizeDocXml(text: string): VbDocXmlToken[] {
  const tokens: VbDocXmlToken[] = [];
  let cursor = 0;
  while (cursor < text.length) {
    const start = text.indexOf("<", cursor);
    if (start === -1) {
      tokens.push({ kind: "text", start: cursor, end: text.length, text: text.slice(cursor) });
      break;
    }
    if (start > cursor) {
      tokens.push({ kind: "text", start: cursor, end: start, text: text.slice(cursor, start) });
    }
    const end = findDocTagEnd(text, start + 1);
    if (end === -1) {
      tokens.push({ kind: "text", start, end: text.length, text: text.slice(start) });
      break;
    }
    const raw = text.slice(start + 1, end).trim();
    const closing = raw.startsWith("/");
    const body = closing ? raw.slice(1).trim() : raw;
    const name = /^([A-Za-z][A-Za-z0-9_-]*)/.exec(body)?.[1];
    if (!name) {
      tokens.push({ kind: "text", start, end: end + 1, text: text.slice(start, end + 1) });
      cursor = end + 1;
      continue;
    }
    if (closing) {
      tokens.push({ kind: "end", start, end: end + 1, name });
    } else {
      const selfClosing = /\/\s*$/.test(body);
      const attributeText = body.slice(name.length).replace(/\/\s*$/, "");
      tokens.push({
        kind: "start",
        start,
        end: end + 1,
        name,
        attributes: parseDocAttributes(attributeText),
        selfClosing,
      });
    }
    cursor = end + 1;
  }
  return tokens;
}

function tokenizeDocTags(text: string): VbDocTagToken[] {
  return tokenizeDocXml(text).flatMap((token) => {
    if (token.kind === "text") {
      return [];
    }
    return {
      kind: token.kind,
      name: token.name,
      selfClosing: token.kind === "start" ? token.selfClosing : false,
    };
  });
}

function findDocTagEnd(text: string, offset: number): number {
  let quote: string | undefined;
  for (let index = offset; index < text.length; index += 1) {
    const char = text[index];
    if (quote) {
      if (char === quote) {
        quote = undefined;
      }
      continue;
    }
    if (char === '"' || char === "'") {
      quote = char;
      continue;
    }
    if (char === ">") {
      return index;
    }
  }
  return -1;
}

function parseDocAttributes(text: string): Record<string, string> {
  const attributes: Record<string, string> = {};
  const pattern = /([A-Za-z][A-Za-z0-9_-]*)\s*=\s*("[^"]*"|'[^']*'|[^\s"'>/]+)/g;
  for (const match of text.matchAll(pattern)) {
    const rawValue = match[2] ?? "";
    const value =
      (rawValue.startsWith('"') && rawValue.endsWith('"')) ||
      (rawValue.startsWith("'") && rawValue.endsWith("'"))
        ? rawValue.slice(1, -1)
        : rawValue;
    attributes[match[1].toLowerCase()] = decodeXmlEntities(value);
  }
  return attributes;
}

function docElements(root: VbDocElement, name: string): VbDocElement[] {
  return root.children.filter(
    (child): child is VbDocElement =>
      typeof child !== "string" && child.name.toLowerCase() === name.toLowerCase(),
  );
}

function firstDocElementText(
  root: VbDocElement,
  name: string,
  preserveTags = false,
): string | undefined {
  const element = docElements(root, name)[0];
  if (!element) {
    return undefined;
  }
  return docElementText(element, preserveTags);
}

function docElementText(element: VbDocElement, preserveCode = false): string {
  return normalizeDocText(docChildrenText(element.children, preserveCode));
}

function docChildrenText(children: Array<VbDocElement | string>, preserveCode = false): string {
  return children
    .map((child) => {
      if (typeof child === "string") {
        return decodeXmlEntities(child);
      }
      if (child.name === "c") {
        return `\`${docChildrenText(child.children, true).trim()}\``;
      }
      if (child.name === "code" && preserveCode) {
        return docChildrenText(child.children, true);
      }
      if (child.name === "see" || child.name === "seealso") {
        return child.attributes.langword ?? child.attributes.cref ?? child.attributes.href ?? "";
      }
      if (child.name === "para") {
        return `\n\n${docChildrenText(child.children, preserveCode)}\n\n`;
      }
      if (child.name === "list") {
        return docChildrenText(child.children, preserveCode);
      }
      return docChildrenText(child.children, preserveCode);
    })
    .join("");
}

function normalizeDocText(text: string): string {
  return text
    .replace(/[ \t]+\n/g, "\n")
    .replace(/\n{3,}/g, "\n\n")
    .trim();
}

function decodeXmlEntities(text: string): string {
  return text
    .replaceAll("&lt;", "<")
    .replaceAll("&gt;", ">")
    .replaceAll("&quot;", '"')
    .replaceAll("&apos;", "'")
    .replaceAll("&amp;", "&");
}

function hasDocumentationContent(documentation: VbDocumentation): boolean {
  return Boolean(
    documentation.summary ||
    documentation.remarks ||
    Object.keys(documentation.params).length > 0 ||
    documentation.returns ||
    documentation.value ||
    documentation.exceptions.length > 0 ||
    documentation.see.length > 0 ||
    documentation.seealso.length > 0 ||
    documentation.example ||
    documentation.code,
  );
}

function documentationMarkdown(
  documentation: VbDocumentation | undefined,
  locale: AspLocale | undefined = undefined,
): string | undefined {
  if (!documentation) {
    return undefined;
  }
  const localizer = createLocalizer(locale);
  const lines: string[] = [];
  if (documentation.summary) {
    lines.push(documentation.summary);
  }
  if (documentation.remarks) {
    lines.push(`**${localizer.t("vb.doc.heading.remarks")}**\n\n${documentation.remarks}`);
  }
  const params = Object.entries(documentation.params);
  if (params.length > 0) {
    lines.push(
      [
        `**${localizer.t("vb.doc.heading.parameters")}**`,
        ...params.map(([name, text]) => `- \`${name}\`: ${text}`),
      ].join("\n"),
    );
  }
  if (documentation.returns) {
    lines.push(`**${localizer.t("vb.doc.heading.returns")}**\n\n${documentation.returns}`);
  }
  if (documentation.value) {
    lines.push(`**${localizer.t("vb.doc.heading.value")}**\n\n${documentation.value}`);
  }
  if (documentation.exceptions.length > 0) {
    lines.push(
      [
        `**${localizer.t("vb.doc.heading.exceptions")}**`,
        ...documentation.exceptions.map((item) =>
          item.cref ? `- \`${item.cref}\`: ${item.text}` : `- ${item.text}`,
        ),
      ].join("\n"),
    );
  }
  if (documentation.see.length > 0 || documentation.seealso.length > 0) {
    lines.push(
      [
        `**${localizer.t("vb.doc.heading.seeAlso")}**`,
        ...[...documentation.see, ...documentation.seealso].map((item) => {
          const target = item.text || item.cref || item.href || item.langword || "";
          return target.startsWith("http") ? `- ${target}` : `- \`${target}\``;
        }),
      ].join("\n"),
    );
  }
  if (documentation.example) {
    lines.push(`**${localizer.t("vb.doc.heading.example")}**\n\n${documentation.example}`);
  }
  if (documentation.code) {
    lines.push(`\`\`\`vbscript\n${documentation.code}\n\`\`\``);
  }
  return lines.filter(Boolean).join("\n\n");
}

function builtinDocumentationMarkdown(
  documentationSpec: BuiltinDocumentationSpec | undefined,
  locale: AspLocale | undefined = undefined,
): string | undefined {
  const documentation = localizedBuiltinDocumentation(documentationSpec, locale);
  return documentationMarkdown(documentation, locale);
}

function localizedBuiltinDocumentation(
  documentationSpec: BuiltinDocumentationSpec | undefined,
  locale: AspLocale | undefined = undefined,
): VbDocumentation | undefined {
  if (!documentationSpec) {
    return undefined;
  }
  const params = Object.fromEntries(
    Object.entries(documentationSpec.parameters ?? {})
      .map(([name, value]) => [name, localizedText(value, locale)])
      .filter(([, value]) => value),
  );
  const documentation: VbDocumentation = {
    summary: localizedText(documentationSpec.summary, locale) || undefined,
    remarks: localizedText(documentationSpec.remarks, locale) || undefined,
    returns: localizedText(documentationSpec.returns, locale) || undefined,
    value: localizedText(documentationSpec.value, locale) || undefined,
    params,
    exceptions: [],
    see: [],
    seealso: [],
  };
  return hasDocumentationContent(documentation) ? documentation : undefined;
}

function appendDocumentationMarkdown(
  base: string,
  documentation: VbDocumentation | undefined,
  locale: AspLocale | undefined = undefined,
): string {
  const markdown = documentationMarkdown(documentation, locale);
  return markdown ? `${base}\n\n${markdown}` : base;
}

function documentationParameterText(
  documentation: VbDocumentation | undefined,
  name: string,
): string | undefined {
  return Object.entries(documentation?.params ?? {}).find(
    ([parameter]) => parameter.toLowerCase() === name.toLowerCase(),
  )?.[1];
}

function documentationTypeNoteForDeclarationHover(
  parsed: AspParsedDocument,
  token: VbToken,
  symbol: VbSymbol,
  symbols: VbSymbol[],
  locale: AspLocale | undefined,
): string | undefined {
  if (
    !symbol.documentation ||
    !sameRange(rangeFromOffsets(parsed.text, token.start, token.end), symbol.range)
  ) {
    return undefined;
  }
  return symbolNeedsDocumentationTypeNote(parsed, symbol, symbols)
    ? createLocalizer(locale).t("vb.doc.typeNote")
    : undefined;
}

function symbolNeedsDocumentationTypeNote(
  parsed: AspParsedDocument,
  symbol: VbSymbol,
  symbols: VbSymbol[],
): boolean {
  const annotations = parseTypeAnnotations(parsed);
  if (symbol.kind === "variable" || symbol.kind === "field" || symbol.kind === "constant") {
    return !hasTypeAnnotation(parsed, symbol, symbols, annotations);
  }
  if (symbol.kind === "sub" || symbol.kind === "function" || symbol.kind === "method") {
    return (
      hasMissingParameterTypeAnnotation(symbol, annotations) ||
      hasMissingReturnTypeAnnotation(symbol, annotations)
    );
  }
  return false;
}

function hasTypeAnnotation(
  parsed: AspParsedDocument,
  symbol: VbSymbol,
  symbols: VbSymbol[],
  annotations: TypeAnnotations,
): boolean {
  return annotations.types.some((annotation) => {
    if (annotation.name.toLowerCase() !== symbol.name.toLowerCase()) {
      return false;
    }
    const candidates = symbols.filter(
      (candidate) => candidate.name.toLowerCase() === annotation.name.toLowerCase(),
    );
    const target =
      candidates.find((candidate) =>
        isSymbolVisibleAt(candidate, parsed.uri, parsed.text, annotation.offset),
      ) ?? candidates[0];
    return target ? sameSymbol(target, symbol) : false;
  });
}

function hasMissingParameterTypeAnnotation(
  symbol: VbSymbol,
  annotations: TypeAnnotations,
): boolean {
  const parameters = parameterDetails(symbol);
  return parameters.some(
    (parameter) =>
      !annotations.params.some(
        (annotation) =>
          annotation.name.toLowerCase() === parameter.name.toLowerCase() &&
          (!annotation.procedureName ||
            annotation.procedureName.toLowerCase() === symbol.name.toLowerCase()),
      ),
  );
}

function hasMissingReturnTypeAnnotation(symbol: VbSymbol, annotations: TypeAnnotations): boolean {
  if (
    !(
      symbol.kind === "function" ||
      (symbol.kind === "method" && symbol.procedureKind === "function")
    )
  ) {
    return false;
  }
  return !annotations.returns.some(
    (annotation) => annotation.name.toLowerCase() === symbol.name.toLowerCase(),
  );
}

export function getVbscriptDocumentationQuickAction(
  parsed: AspParsedDocument,
  position: Position,
  context: VbProjectContext = {},
): VbDocumentationQuickAction | undefined {
  const symbols = context.symbols ?? collectVbscriptSymbols(parsed, context);
  const symbol = documentationActionSymbolAt(parsed, position, symbols);
  if (!symbol) {
    return undefined;
  }
  const owner = symbol.kind === "parameter" ? callableOwnerForParameter(symbol, symbols) : symbol;
  if (!owner) {
    return undefined;
  }
  const ownerNode = nodeForSymbol(parsed, owner);
  const targetNode = symbol.kind === "parameter" ? ownerNode : nodeForSymbol(parsed, symbol);
  if (!ownerNode || !targetNode) {
    return undefined;
  }
  const docs = docCommentBlockBefore(parsed, ownerNode.start);
  const annotations = parseTypeAnnotations(parsed);
  const xmlLines = missingDocumentationXmlLines(symbol, owner, targetNode, docs);
  const annotationLines = missingDocumentationAnnotationLines(
    parsed,
    symbol,
    owner,
    symbols,
    annotations,
  );
  const edits = documentationInsertEdits(
    parsed.text,
    ownerNode.start,
    docs,
    annotationLines,
    xmlLines,
  );
  return edits.length > 0 ? { symbol, edits } : undefined;
}

function documentationActionSymbolAt(
  parsed: AspParsedDocument,
  position: Position,
  symbols: VbSymbol[],
): VbSymbol | undefined {
  const offset = offsetAt(parsed.text, position);
  return symbols
    .filter(
      (symbol) =>
        symbol.sourceUri === parsed.uri &&
        !symbol.implicit &&
        isDocumentationActionSymbol(symbol) &&
        rangeContainsOffset(parsed.text, symbol.range, offset),
    )
    .sort((left, right) => rangeSize(left.range) - rangeSize(right.range))[0];
}

function isDocumentationActionSymbol(symbol: VbSymbol): boolean {
  return [
    "class",
    "function",
    "sub",
    "method",
    "property",
    "variable",
    "field",
    "constant",
    "parameter",
  ].includes(symbol.kind);
}

function callableOwnerForParameter(parameter: VbSymbol, symbols: VbSymbol[]): VbSymbol | undefined {
  return symbols.find(
    (candidate) =>
      candidate.sourceUri === parameter.sourceUri &&
      candidate.scopeRange &&
      parameter.scopeRange &&
      sameRange(candidate.scopeRange, parameter.scopeRange) &&
      (candidate.kind === "function" ||
        candidate.kind === "sub" ||
        candidate.kind === "method" ||
        candidate.kind === "property"),
  );
}

function nodeForSymbol(parsed: AspParsedDocument, symbol: VbSymbol): VbCstNode | undefined {
  for (const document of vbDocuments(parsed)) {
    for (const node of flattenVbNodes(document)) {
      if (
        node.nameToken &&
        sameRange(
          rangeFromOffsets(parsed.text, node.nameToken.start, node.nameToken.end),
          symbol.range,
        )
      ) {
        return node;
      }
      if (
        node.identifiers?.some((identifier) =>
          sameRange(rangeFromOffsets(parsed.text, identifier.start, identifier.end), symbol.range),
        )
      ) {
        return node;
      }
      if (
        symbol.kind === "parameter" &&
        node.parameterMetadata?.some((parameter) =>
          sameRange(
            rangeFromOffsets(parsed.text, parameter.token.start, parameter.token.end),
            symbol.range,
          ),
        )
      ) {
        return node;
      }
    }
  }
  return undefined;
}

function missingDocumentationXmlLines(
  symbol: VbSymbol,
  owner: VbSymbol,
  targetNode: VbCstNode,
  docs: VbToken[],
): string[] {
  const documentation = owner.documentation;
  const lines: string[] = [];
  if (symbol.kind === "parameter") {
    if (!documentationParameterText(documentation, symbol.name)) {
      lines.push(`''' <param name="${symbol.name}">TODO: Describe ${symbol.name}.</param>`);
    }
    return lines;
  }
  if (canGenerateOwnXmlDocumentation(symbol, targetNode) && !documentation?.summary) {
    lines.push(`''' <summary>TODO: Describe ${symbol.name}.</summary>`);
  }
  if (isCallableDocumentationSymbol(symbol)) {
    for (const parameter of parameterDetails(symbol)) {
      if (!documentationParameterText(documentation, parameter.name)) {
        lines.push(`''' <param name="${parameter.name}">TODO: Describe ${parameter.name}.</param>`);
      }
    }
    if (callableHasReturnValue(symbol) && !documentation?.returns) {
      lines.push("''' <returns>TODO: Describe return value.</returns>");
    }
  }
  if (symbolHasDocumentedValue(symbol) && !documentation?.value) {
    lines.push(`''' <value>TODO: Describe ${symbol.name}.</value>`);
  }
  return docs.length > 0 || canGenerateOwnXmlDocumentation(symbol, targetNode) ? lines : [];
}

function canGenerateOwnXmlDocumentation(symbol: VbSymbol, node: VbCstNode): boolean {
  if (symbol.kind === "variable" || symbol.kind === "field" || symbol.kind === "constant") {
    return (node.identifiers?.length ?? 0) === 1;
  }
  return symbol.kind !== "parameter";
}

function isCallableDocumentationSymbol(symbol: VbSymbol): boolean {
  return (
    symbol.kind === "function" ||
    symbol.kind === "sub" ||
    symbol.kind === "method" ||
    symbol.kind === "property"
  );
}

function callableHasReturnValue(symbol: VbSymbol): boolean {
  return (
    symbol.kind === "function" ||
    (symbol.kind === "method" && symbol.procedureKind === "function") ||
    (symbol.kind === "property" && symbol.propertyAccessor === "get")
  );
}

function symbolHasDocumentedValue(symbol: VbSymbol): boolean {
  return (
    symbol.kind === "variable" ||
    symbol.kind === "field" ||
    symbol.kind === "constant" ||
    symbol.kind === "property"
  );
}

function missingDocumentationAnnotationLines(
  parsed: AspParsedDocument,
  symbol: VbSymbol,
  owner: VbSymbol,
  symbols: VbSymbol[],
  annotations: TypeAnnotations,
): string[] {
  const lines: string[] = [];
  if (symbol.kind === "parameter") {
    if (!hasParameterTypeAnnotation(owner, symbol.name, annotations)) {
      lines.push(`' @param ${owner.name}.${symbol.name} As ${symbolTypeName(symbol)}`);
    }
    return lines;
  }
  if (symbol.kind === "variable" || symbol.kind === "field" || symbol.kind === "constant") {
    if (!hasTypeAnnotation(parsed, symbol, symbols, annotations)) {
      lines.push(`' @type ${symbol.name} As ${symbolTypeName(symbol)}`);
    }
    return lines;
  }
  if (isCallableDocumentationSymbol(symbol)) {
    for (const parameter of parameterDetails(symbol)) {
      if (!hasParameterTypeAnnotation(symbol, parameter.name, annotations)) {
        const parameterSymbol = parameterSymbolForCallable(symbol, parameter.name, symbols);
        lines.push(
          `' @param ${symbol.name}.${parameter.name} As ${symbolTypeName(parameterSymbol)}`,
        );
      }
    }
    if (callableHasReturnValue(symbol) && !hasReturnTypeAnnotation(symbol, annotations)) {
      lines.push(`' @returns ${symbol.name} ${symbolTypeName(symbol)}`);
    }
  }
  return lines;
}

function hasParameterTypeAnnotation(
  callable: VbSymbol,
  parameterName: string,
  annotations: TypeAnnotations,
): boolean {
  return annotations.params.some(
    (annotation) =>
      annotation.name.toLowerCase() === parameterName.toLowerCase() &&
      (!annotation.procedureName ||
        annotation.procedureName.toLowerCase() === callable.name.toLowerCase()),
  );
}

function hasReturnTypeAnnotation(symbol: VbSymbol, annotations: TypeAnnotations): boolean {
  return annotations.returns.some(
    (annotation) => annotation.name.toLowerCase() === symbol.name.toLowerCase(),
  );
}

function parameterSymbolForCallable(
  callable: VbSymbol,
  parameterName: string,
  symbols: VbSymbol[],
): VbSymbol | undefined {
  return symbols.find(
    (candidate) =>
      candidate.kind === "parameter" &&
      candidate.name.toLowerCase() === parameterName.toLowerCase() &&
      candidate.sourceUri === callable.sourceUri &&
      candidate.scopeRange &&
      callable.scopeRange &&
      sameRange(candidate.scopeRange, callable.scopeRange),
  );
}

function symbolTypeName(symbol: VbSymbol | undefined): string {
  if (!symbol) {
    return "Variant";
  }
  if (symbol.type) {
    return formatTypeRef(symbol.type);
  }
  return symbol.typeName ?? "Variant";
}

function documentationInsertEdits(
  text: string,
  declarationOffset: number,
  docs: VbToken[],
  annotationLines: string[],
  xmlLines: string[],
): TextEdit[] {
  if (annotationLines.length === 0 && xmlLines.length === 0) {
    return [];
  }
  const newLine = preferredNewLine(text);
  const declarationLine = positionAt(text, declarationOffset).line;
  const indent = lineIndent(text, declarationLine);
  if (docs.length === 0) {
    const insertPosition = { line: declarationLine, character: 0 };
    return [
      {
        range: { start: insertPosition, end: insertPosition },
        newText:
          [...annotationLines, ...xmlLines].map((line) => `${indent}${line}`).join(newLine) +
          newLine,
      },
    ];
  }
  const firstDocLine = positionAt(text, docs[0].start).line;
  const replaceStart = lineStartOffset(text, firstDocLine);
  const replaceEnd = docs.at(-1)!.end;
  const existingDocText = text.slice(replaceStart, replaceEnd);
  const replacement = [
    ...annotationLines.map((line) => `${indent}${line}`),
    existingDocText,
    ...xmlLines.map((line) => `${indent}${line}`),
  ].join(newLine);
  return [
    {
      range: {
        start: positionAt(text, replaceStart),
        end: positionAt(text, replaceEnd),
      },
      newText: replacement,
    },
  ];
}

function preferredNewLine(text: string): string {
  return text.includes("\r\n") ? "\r\n" : "\n";
}

function lineIndent(text: string, line: number): string {
  const start = lineStartOffset(text, line);
  const end = lineEndOffset(text, start);
  return /^[ \t]*/.exec(text.slice(start, end))?.[0] ?? "";
}

function lineStartOffset(text: string, line: number): number {
  let currentLine = 0;
  for (let index = 0; index < text.length; index += 1) {
    if (currentLine === line) {
      return index;
    }
    if (text.charCodeAt(index) === 10) {
      currentLine += 1;
    }
  }
  return text.length;
}

function lineEndOffset(text: string, start: number): number {
  const end = text.indexOf("\n", start);
  return end === -1 ? text.length : end;
}

function nextProcedureName(parsed: AspParsedDocument, offset: number): string | undefined {
  return vbDocuments(parsed)
    .flatMap((document) => flattenVbNodes(document))
    .filter(
      (node) => node.start >= offset && (node.kind === "Procedure" || node.kind === "Property"),
    )
    .sort((left, right) => left.start - right.start)[0]?.nameToken?.text;
}

function vbStatements(parsed: AspParsedDocument): VbToken[][] {
  const statements: VbToken[][] = [];
  for (const document of vbDocuments(parsed)) {
    let current: VbToken[] = [];
    for (const token of document.tokens.filter(
      (item) => item.kind !== "whitespace" && item.kind !== "comment",
    )) {
      if (token.kind === "newline" || token.text === ":") {
        if (current.length > 0) {
          statements.push(current);
          current = [];
        }
        continue;
      }
      current.push(token);
    }
    if (current.length > 0) {
      statements.push(current);
    }
  }
  return statements;
}

function inferExpressionType(
  parsed: AspParsedDocument,
  tokens: VbToken[],
  symbols: VbSymbol[],
  env: VbTypeEnvironment,
  offset: number,
): VbTypeRef | undefined {
  const significant = tokens.filter((token) => !isTriviaToken(token));
  return inferSignificantExpressionType(parsed, significant, symbols, env, offset);
}

function inferSignificantExpressionType(
  parsed: AspParsedDocument,
  significant: VbToken[],
  symbols: VbSymbol[],
  env: VbTypeEnvironment,
  offset: number,
): VbTypeRef | undefined {
  const expression = trimOuterParens(significant);
  const binary = splitByLowestPrecedenceOperator(expression);
  if (binary) {
    const left = inferSignificantExpressionType(parsed, binary.left, symbols, env, offset);
    const right = inferSignificantExpressionType(parsed, binary.right, symbols, env, offset);
    return inferBinaryExpressionType(binary.operator, left, right);
  }
  const first = expression[0];
  if (!first) {
    return undefined;
  }
  if (first.kind === "string") {
    return typeRef("String");
  }
  if (first.kind === "number") {
    return typeRef("Number");
  }
  if (first.text === "#" && expression.at(-1)?.text === "#") {
    return typeRef("Date");
  }
  const lower = first.text.toLowerCase();
  if (lower === "true" || lower === "false") {
    return typeRef("Boolean");
  }
  if (lower === "nothing" || lower === "null" || lower === "empty") {
    return typeRef(canonicalBuiltinTypeName(lower));
  }
  if (lower === "array" && expression[1]?.text === "(") {
    return typeRef("Array");
  }
  if (lower === "new" && expression[1]?.kind === "identifier") {
    return typeRef(expression[1].text);
  }
  const createObjectIndex = findCreateObjectCall(expression, 0, expression.length - 1);
  if (createObjectIndex === 0) {
    const stringToken = expression
      .slice(createObjectIndex)
      .find((token) => token.kind === "string");
    return stringToken
      ? typeRef(stringToken.value ?? unquoteVbString(stringToken.text))
      : undefined;
  }
  if (
    first.kind === "identifier" &&
    expression[1]?.text === "." &&
    expression[2]?.kind === "identifier"
  ) {
    const ownerType =
      inferVariableTypeRef(first.text, parsed, offset, symbols) ??
      classicAspObjectTypeRef(first.text);
    return ownerType
      ? (memberReturnType(ownerType, expression[2].text, env) ??
          memberType(ownerType, expression[2].text, env))
      : undefined;
  }
  if (first.kind === "identifier") {
    const called = expression[1]?.text === "(";
    if (called) {
      const builtin = builtinSignature(first.text);
      if (builtin) {
        return builtin.returnType;
      }
      const symbol = visibleSymbols(parsed, offset, symbols).find(
        (candidate) =>
          candidate.name.toLowerCase() === first.text.toLowerCase() &&
          (candidate.kind === "function" ||
            candidate.kind === "method" ||
            candidate.kind === "property"),
      );
      return symbol?.type ?? (symbol?.typeName ? typeRef(symbol.typeName) : undefined);
    }
    const constant = builtinConstant(first.text);
    if (constant) {
      return typeRef(constant.type);
    }
    return inferVariableTypeRef(first.text, parsed, offset, symbols);
  }
  return undefined;
}

function trimOuterParens(tokens: VbToken[]): VbToken[] {
  let result = tokens;
  while (result[0]?.text === "(" && result.at(-1)?.text === ")") {
    const closeIndex = matchingCloseParen(result, 0);
    if (closeIndex !== result.length - 1) {
      break;
    }
    result = result.slice(1, -1);
  }
  return result;
}

function splitByLowestPrecedenceOperator(
  tokens: VbToken[],
): { left: VbToken[]; operator: string; right: VbToken[] } | undefined {
  const operators = [
    ["or", "xor", "eqv", "imp"],
    ["and"],
    ["=", "<>", "<", ">", "<=", ">=", "is"],
    ["&"],
    ["+", "-"],
    ["mod"],
    ["*", "/"],
    ["\\"],
    ["^"],
  ];
  for (const group of operators) {
    let depth = 0;
    for (let index = tokens.length - 1; index >= 0; index -= 1) {
      const token = tokens[index];
      if (token.text === ")") {
        depth += 1;
        continue;
      }
      if (token.text === "(") {
        depth -= 1;
        continue;
      }
      const operator = token.text.toLowerCase();
      if (depth === 0 && group.includes(operator) && index > 0 && index < tokens.length - 1) {
        return {
          left: tokens.slice(0, index),
          operator,
          right: tokens.slice(index + 1),
        };
      }
    }
  }
  return undefined;
}

function inferBinaryExpressionType(
  operator: string,
  left: VbTypeRef | undefined,
  right: VbTypeRef | undefined,
): VbTypeRef | undefined {
  if (
    ["=", "<>", "<", ">", "<=", ">=", "is", "and", "or", "xor", "eqv", "imp"].includes(operator)
  ) {
    return typeRef("Boolean");
  }
  if (operator === "&") {
    return typeRef("String");
  }
  if (operator === "+" && (typeIncludesName(left, "String") || typeIncludesName(right, "String"))) {
    return typeRef("String");
  }
  if (["+", "-", "*", "/", "\\", "mod", "^"].includes(operator)) {
    return typeRef("Number");
  }
  return left ?? right;
}

function typeIncludesName(type: VbTypeRef | undefined, name: string): boolean {
  return Boolean(
    type && expandUnionType(type).some((item) => item.name.toLowerCase() === name.toLowerCase()),
  );
}

function applyTypeAnnotations(parsed: AspParsedDocument, symbols: VbSymbol[]): void {
  const annotations = parseTypeAnnotations(parsed);
  for (const annotation of annotations.types) {
    const symbol =
      visibleSymbols(parsed, annotation.offset, symbols).find(
        (candidate) => candidate.name.toLowerCase() === annotation.name.toLowerCase(),
      ) ??
      symbols.find((candidate) => candidate.name.toLowerCase() === annotation.name.toLowerCase());
    if (symbol) {
      setSymbolType(symbol, annotation.typeName, true);
    }
  }
  for (const annotation of annotations.params) {
    const symbol = symbols.find(
      (candidate) =>
        (candidate.kind === "variable" || candidate.kind === "parameter") &&
        candidate.name.toLowerCase() === annotation.name.toLowerCase() &&
        (!annotation.procedureName ||
          candidate.scopeName?.toLowerCase() === annotation.procedureName.toLowerCase()),
    );
    if (symbol) {
      setSymbolType(symbol, annotation.typeName, true);
    }
  }
  for (const annotation of annotations.returns) {
    const symbol = symbols.find(
      (candidate) =>
        (candidate.kind === "function" || candidate.kind === "method") &&
        candidate.name.toLowerCase() === annotation.name.toLowerCase(),
    );
    if (symbol) {
      setSymbolType(symbol, annotation.typeName, true);
    }
  }
}

function applyVariantFallbackTypes(symbols: VbSymbol[]): void {
  for (const symbol of symbols) {
    if (symbolTypeRef(symbol)) {
      continue;
    }
    if (
      symbol.kind === "variable" ||
      symbol.kind === "constant" ||
      symbol.kind === "field" ||
      symbol.kind === "parameter" ||
      symbol.kind === "function" ||
      symbol.kind === "property" ||
      (symbol.kind === "method" && symbol.procedureKind !== "sub")
    ) {
      setSymbolType(symbol, "Variant");
    }
  }
}

function vbDocuments(parsed: AspParsedDocument): VbCstNode[] {
  const documents: VbCstNode[] = [];
  const visit = (node: AspCstNode): void => {
    if (node.vbscript) {
      documents.push(node.vbscript);
    }
    for (const child of node.children ?? []) {
      visit(child);
    }
  };
  visit(parsed.cst);
  if (documents.length === 0) {
    for (const region of serverRegions(parsed)) {
      documents.push(
        parseVbscriptCst(
          parsed.text.slice(region.contentStart, region.contentEnd),
          parsed.text,
          region.contentStart,
        ),
      );
    }
  }
  return documents;
}

function flattenVbNodes(node: VbCstNode): VbCstNode[] {
  return [node, ...node.children.flatMap((child) => flattenVbNodes(child))];
}

function parentClassName(parsed: AspParsedDocument, offset: number): string | undefined {
  return enclosingVbNodes(parsed, offset)
    .reverse()
    .find((node) => node.kind === "Class")?.nameToken?.text;
}

function scopeNodeAt(parsed: AspParsedDocument, offset: number): VbCstNode | undefined {
  return enclosingVbNodes(parsed, offset)
    .reverse()
    .find((node) => node.kind === "Procedure" || node.kind === "Property");
}

function enclosingVbNodes(parsed: AspParsedDocument, offset: number): VbCstNode[] {
  const result: VbCstNode[] = [];
  const visit = (node: VbCstNode): void => {
    if (offset < node.start || offset > node.end) {
      return;
    }
    result.push(node);
    for (const child of node.children) {
      visit(child);
    }
  };
  for (const document of vbDocuments(parsed)) {
    visit(document);
  }
  return result;
}

function currentClassName(
  parsed: AspParsedDocument,
  offset: number,
  symbols: VbSymbol[],
): string | undefined {
  return symbols.find(
    (symbol) =>
      symbol.kind === "class" &&
      symbol.sourceUri === parsed.uri &&
      rangeContainsOffset(parsed.text, symbol.scopeRange, offset),
  )?.name;
}

function inferVariableTypeRef(
  name: string,
  parsed: AspParsedDocument,
  offset: number,
  symbols: VbSymbol[],
): VbTypeRef | undefined {
  return visibleSymbols(parsed, offset, symbols)
    .filter((symbol) => symbol.name.toLowerCase() === name.toLowerCase())
    .sort(
      (left, right) =>
        Number(Boolean(symbolTypeRef(right))) - Number(Boolean(symbolTypeRef(left))) ||
        symbolPriority(right) - symbolPriority(left),
    )
    .map(symbolTypeRef)[0];
}

function currentWithTypeRef(
  parsed: AspParsedDocument,
  offset: number,
  symbols: VbSymbol[],
): VbTypeRef | undefined {
  const withNode = enclosingVbNodes(parsed, offset)
    .reverse()
    .find((node) => node.kind === "With" && node.nameToken);
  return withNode?.nameToken
    ? inferVariableTypeRef(withNode.nameToken.text, parsed, offset, symbols)
    : undefined;
}

function currentClassTypeRef(
  parsed: AspParsedDocument,
  offset: number,
  symbols: VbSymbol[],
): VbTypeRef | undefined {
  const name = currentClassName(parsed, offset, symbols);
  return name ? typeRef(name) : undefined;
}

function typeMemberCompletions(
  type: VbTypeRef,
  symbols: VbSymbol[],
  env: VbTypeEnvironment,
): CompletionItem[] {
  const memberSets = expandUnionType(typeWithoutNothing(type) ?? type).map((candidate) =>
    dedupeCompletions(typeMemberCompletionsForName(candidate.name, symbols, env)),
  );
  if (memberSets.length === 0) {
    return [];
  }
  const [first, ...rest] = memberSets;
  return first.filter((item) =>
    rest.every((set) =>
      set.some((candidate) => candidate.label.toLowerCase() === item.label.toLowerCase()),
    ),
  );
}

function typeMemberCompletionsForName(
  typeName: string,
  symbols: VbSymbol[],
  env: VbTypeEnvironment,
): CompletionItem[] {
  const type = findType(env, typeName);
  return [
    ...(type?.members.map(memberToCompletion) ?? []),
    ...symbols
      .filter(
        (symbol) =>
          symbol.memberOf?.toLowerCase() === typeName.toLowerCase() &&
          (symbol.kind === "method" || symbol.kind === "field" || symbol.kind === "property"),
      )
      .map((symbol) => symbolToCompletion(symbol)),
    ...(externalObjectMembers[typeName.toLowerCase()] ?? []),
  ];
}

function memberToCompletion(member: VbMember): CompletionItem {
  return {
    label: member.name,
    kind:
      member.kind === "method"
        ? CompletionItemKind.Method
        : member.kind === "event"
          ? CompletionItemKind.Event
          : member.kind === "field"
            ? CompletionItemKind.Field
            : CompletionItemKind.Property,
    detail: member.type ? `${member.kind} As ${formatTypeRef(member.type)}` : member.kind,
  };
}

function builtinMemberDescription(
  parsed: AspParsedDocument,
  offset: number,
  symbols: VbSymbol[],
  env: VbTypeEnvironment,
  locale: AspLocale | undefined,
): string | undefined {
  const access = memberAccessAt(parsed, offset);
  if (!access) {
    return undefined;
  }
  const ownerType =
    access.owner === ""
      ? currentWithTypeRef(parsed, offset, symbols)
      : access.owner.toLowerCase() === "me"
        ? currentClassTypeRef(parsed, offset, symbols)
        : (inferVariableTypeRef(access.owner, parsed, offset, symbols) ??
          classicAspObjectTypeRef(access.owner));
  if (!ownerType) {
    return undefined;
  }
  for (const candidate of expandUnionType(ownerType)) {
    const type = findType(env, candidate.name);
    const member = type?.members.find(
      (item) => item.name.toLowerCase() === access.member.toLowerCase(),
    );
    if (!type || !member) {
      continue;
    }
    const signature = member.signature
      ? signatureLabelFromMember(type.name, member.name, member.signature)
      : undefined;
    const typeSuffix = member.type ? ` As ${formatTypeRef(member.type)}` : "";
    return appendBuiltinDocumentation(
      markdownHover(signature ?? `${member.kind} ${type.name}.${member.name}${typeSuffix}`),
      builtinMemberSpecForType(type.name, member.name)?.documentation,
      locale,
    );
  }
  return undefined;
}

function visibleSymbols(
  parsed: AspParsedDocument,
  offset: number,
  symbols: VbSymbol[],
): VbSymbol[] {
  return symbols.filter((symbol) => isSymbolVisibleAt(symbol, parsed.uri, parsed.text, offset));
}

function isSymbolVisibleAt(
  symbol: VbSymbol,
  uri: string,
  sourceText: string,
  offset: number,
): boolean {
  if (symbol.sourceUri !== uri) {
    return !symbol.scopeName && !symbol.memberOf;
  }
  if (
    (symbol.kind === "class" || symbol.kind === "function" || symbol.kind === "sub") &&
    !symbol.memberOf
  ) {
    return true;
  }
  if (!symbol.scopeRange) {
    return true;
  }
  return rangeContainsOffset(sourceText, symbol.scopeRange, offset);
}

function resolveSymbolAt(
  parsed: AspParsedDocument,
  offset: number,
  symbols: VbSymbol[],
): VbSymbol | undefined {
  const member = memberAccessAt(parsed, offset);
  if (member) {
    const type =
      member.owner === ""
        ? currentWithTypeRef(parsed, offset, symbols)
        : member.owner.toLowerCase() === "me"
          ? currentClassTypeRef(parsed, offset, symbols)
          : inferVariableTypeRef(member.owner, parsed, offset, symbols);
    return type ? resolveMemberSymbolForType(type, member.member, symbols) : undefined;
  }
  const token = identifierTokenAt(parsed, offset);
  if (!token) {
    return undefined;
  }
  return visibleSymbols(parsed, offset, symbols)
    .filter((symbol) => symbol.name.toLowerCase() === token.text.toLowerCase())
    .sort((left, right) => symbolPriority(right) - symbolPriority(left))[0];
}

function resolveMemberSymbolForType(
  type: VbTypeRef,
  memberName: string,
  symbols: VbSymbol[],
): VbSymbol | undefined {
  const members = expandUnionType(type).map((candidate) =>
    symbols.find(
      (symbol) =>
        symbol.memberOf?.toLowerCase() === candidate.name.toLowerCase() &&
        symbol.name.toLowerCase() === memberName.toLowerCase(),
    ),
  );
  return members.every(Boolean) ? members[0] : undefined;
}

function memberAccessAt(
  parsed: AspParsedDocument,
  offset: number,
): { owner: string; member: string } | undefined {
  const token = identifierTokenAt(parsed, offset);
  if (!token) {
    return undefined;
  }
  const tokens = significantTokens(parsed);
  const index = tokens.findIndex((item) => item.start === token.start && item.end === token.end);
  if (tokens[index - 1]?.text !== ".") {
    return undefined;
  }
  const owner = tokens[index - 2];
  return owner?.kind === "identifier" || owner?.text.toLowerCase() === "me"
    ? { owner: owner.text, member: token.text }
    : { owner: "", member: token.text };
}

function diagnoseDeclarationSyntax(
  parsed: AspParsedDocument,
  locale: AspLocale | undefined,
): Diagnostic[] {
  const localizer = createLocalizer(locale);
  const diagnostics: Diagnostic[] = [];
  for (const node of vbDocuments(parsed).flatMap((document) => flattenVbNodes(document))) {
    if (node.kind !== "VariableDeclaration" || !node.declarationKind) {
      continue;
    }
    const tokens = node.tokens.filter((token) => !isTriviaToken(token));
    if (topLevelToken(tokens, "=")) {
      diagnostics.push(
        declarationSyntaxDiagnostic(
          parsed,
          node,
          localizer.t("vb.diagnostic.initializedDeclaration", {
            keyword: titleCaseKeyword(node.declarationKind),
          }),
          "initializedDeclaration",
        ),
      );
    }
    if (
      (node.declarationKind === "dim" ||
        node.declarationKind === "public" ||
        node.declarationKind === "private") &&
      tokens.some((token) => lowerToken(token) === "as")
    ) {
      diagnostics.push(
        declarationSyntaxDiagnostic(
          parsed,
          node,
          localizer.t("vb.diagnostic.typedDeclaration", {
            keyword: titleCaseKeyword(node.declarationKind),
          }),
          "typedDeclaration",
        ),
      );
    }
  }
  return diagnostics;
}

function diagnoseCallSyntax(
  parsed: AspParsedDocument,
  symbols: VbSymbol[],
  locale: AspLocale | undefined,
): Diagnostic[] {
  const diagnostics: Diagnostic[] = [];
  for (const statement of vbStatements(parsed)) {
    const diagnostic = callSyntaxDiagnosticForStatement(parsed, statement, symbols, locale);
    if (diagnostic) {
      diagnostics.push(diagnostic);
    }
  }
  return diagnostics;
}

function callSyntaxDiagnosticForStatement(
  parsed: AspParsedDocument,
  statement: VbToken[],
  symbols: VbSymbol[],
  locale: AspLocale | undefined,
): Diagnostic | undefined {
  if (lowerToken(statement[0]) === "call") {
    return callKeywordSyntaxDiagnostic(parsed, statement, symbols, locale);
  }
  const assignment = assignmentCallWithoutParentheses(parsed, statement, symbols, locale);
  if (assignment) {
    return assignment;
  }
  return parenthesizedStatementCallDiagnostic(parsed, statement, symbols, locale);
}

function callKeywordSyntaxDiagnostic(
  parsed: AspParsedDocument,
  statement: VbToken[],
  symbols: VbSymbol[],
  locale: AspLocale | undefined,
): Diagnostic | undefined {
  const name = statement[1];
  if (!name || name.kind !== "identifier" || statement[2]?.text === "(" || !statement[2]) {
    return undefined;
  }
  const symbol = userDefinedProcedureSymbol(parsed, name.text, name.start, symbols);
  if (!symbol) {
    return undefined;
  }
  return callSyntaxDiagnostic(
    parsed,
    statement,
    symbol.name,
    "callStatementRequiresParentheses",
    `${parsed.text.slice(statement[0].start, name.end)}(${callArgumentText(parsed, statement, 2)})`,
    locale,
  );
}

function assignmentCallWithoutParentheses(
  parsed: AspParsedDocument,
  statement: VbToken[],
  symbols: VbSymbol[],
  locale: AspLocale | undefined,
): Diagnostic | undefined {
  const equalsIndex = statement.findIndex((token) => token.text === "=");
  const name = statement[equalsIndex + 1];
  if (
    equalsIndex < 1 ||
    statement[0]?.kind !== "identifier" ||
    !name ||
    name.kind !== "identifier" ||
    statement[equalsIndex + 2]?.text === "(" ||
    !statement[equalsIndex + 2]
  ) {
    return undefined;
  }
  const symbol = userDefinedProcedureSymbol(parsed, name.text, name.start, symbols);
  if (!symbol) {
    return undefined;
  }
  return callSyntaxDiagnostic(
    parsed,
    statement,
    symbol.name,
    "expressionCallRequiresParentheses",
    `${parsed.text.slice(statement[0].start, name.end)}(${callArgumentText(
      parsed,
      statement,
      equalsIndex + 2,
    )})`,
    locale,
  );
}

function parenthesizedStatementCallDiagnostic(
  parsed: AspParsedDocument,
  statement: VbToken[],
  symbols: VbSymbol[],
  locale: AspLocale | undefined,
): Diagnostic | undefined {
  const name = statement[0];
  if (
    !name ||
    name.kind !== "identifier" ||
    statement[1]?.text !== "(" ||
    isNonCallStatementStart(name) ||
    vbRegionAt(parsed, name.start)?.kind === "asp-expression"
  ) {
    return undefined;
  }
  const closeIndex = matchingCloseParen(statement, 1);
  if (closeIndex !== statement.length - 1) {
    return undefined;
  }
  const argumentCount = countArguments(statement.slice(2, closeIndex));
  if (argumentCount < 2) {
    return undefined;
  }
  const symbol = userDefinedProcedureSymbol(parsed, name.text, name.start, symbols);
  if (!symbol) {
    return undefined;
  }
  return callSyntaxDiagnostic(
    parsed,
    statement,
    symbol.name,
    "statementCallDisallowsParenthesizedArguments",
    `${parsed.text.slice(statement[0].start, statement[1].start)} ${parsed.text
      .slice(statement[1].end, statement[closeIndex].start)
      .trim()}`,
    locale,
  );
}

function callSyntaxDiagnostic(
  parsed: AspParsedDocument,
  statement: VbToken[],
  name: string,
  code: VbCallSyntaxDiagnosticCode,
  newText: string,
  locale: AspLocale | undefined,
): Diagnostic {
  const start = statement[0].start;
  const end = statement.at(-1)?.end ?? statement[0].end;
  return {
    severity: DiagnosticSeverity.Error,
    range: rangeFromOffsets(parsed.text, start, end),
    message: createLocalizer(locale).t("vb.diagnostic.invalidCallSyntax", { name }),
    code,
    data: {
      fixKind: "vbscriptCallSyntax",
      name,
      newText,
    },
    source: "asp-lsp-vbscript-syntax",
  };
}

function callArgumentText(parsed: AspParsedDocument, statement: VbToken[], startIndex: number) {
  return parsed.text
    .slice(statement[startIndex].start, statement.at(-1)?.end ?? statement[startIndex].end)
    .trim();
}

function userDefinedProcedureSymbol(
  parsed: AspParsedDocument,
  name: string,
  offset: number,
  symbols: VbSymbol[],
): VbSymbol | undefined {
  return visibleSymbols(parsed, offset, symbols).find(
    (candidate) =>
      candidate.name.toLowerCase() === name.toLowerCase() &&
      (candidate.kind === "function" || candidate.kind === "sub"),
  );
}

function topLevelToken(tokens: VbToken[], text: string): VbToken | undefined {
  let depth = 0;
  for (const token of tokens) {
    if (token.text === "(") {
      depth += 1;
    } else if (token.text === ")") {
      depth = Math.max(0, depth - 1);
    } else if (depth === 0 && token.text === text) {
      return token;
    }
  }
  return undefined;
}

function declarationSyntaxDiagnostic(
  parsed: AspParsedDocument,
  node: VbCstNode,
  message: string,
  code: "initializedDeclaration" | "typedDeclaration",
): Diagnostic {
  return {
    severity: DiagnosticSeverity.Error,
    range: rangeFromOffsets(parsed.text, node.start, node.end),
    message,
    code,
    data: {
      declarationKind: node.declarationKind ?? "",
      name: node.identifiers?.[0]?.text ?? "",
    },
    source: "asp-lsp-vbscript-syntax",
  };
}

function diagnoseUndeclaredVariables(
  parsed: AspParsedDocument,
  symbols: VbSymbol[],
  locale: AspLocale | undefined,
): Diagnostic[] {
  const localizer = createLocalizer(locale);
  const declaredBuiltins = new Set([
    "request",
    "response",
    "session",
    "application",
    "server",
    "asperror",
    "true",
    "false",
    "nothing",
    "empty",
    "null",
    "me",
  ]);
  const diagnostics: Diagnostic[] = [];
  for (const token of identifierTokens(parsed)) {
    const name = token.text;
    const lower = name.toLowerCase();
    const previous = previousSignificantToken(parsed, token.start);
    const next = nextSignificantToken(parsed, token.end);
    if (
      declaredBuiltins.has(lower) ||
      visibleSymbols(parsed, token.start, symbols).some(
        (symbol) => symbol.name.toLowerCase() === lower,
      ) ||
      isBuiltinName(name) ||
      isDeclarationNameToken(parsed, token) ||
      previous?.text === "."
    ) {
      continue;
    }
    if (next?.text === "(" && /^[A-Z]/.test(name)) {
      continue;
    }
    diagnostics.push({
      severity: DiagnosticSeverity.Warning,
      range: rangeFromOffsets(parsed.text, token.start, token.end),
      message: localizer.t("vb.diagnostic.undeclared", { name }),
      source: "asp-lsp-vbscript",
    });
  }
  return diagnostics;
}

function diagnoseUnusedSymbols(
  parsed: AspParsedDocument,
  symbols: VbSymbol[],
  context: VbProjectContext,
): Diagnostic[] {
  const diagnostics: Diagnostic[] = [];
  for (const symbol of symbols) {
    if (
      symbol.sourceUri !== parsed.uri ||
      isBuiltinName(symbol.name) ||
      isRuntimeEntryPoint(parsed, symbol) ||
      !isUnusedDiagnosticCandidate(symbol)
    ) {
      continue;
    }
    const references = getVbscriptReferences(parsed, symbol.range.start, {
      ...context,
      symbols,
    }).filter((reference) => !sameRange(reference.range, symbol.range));
    if (references.length > 0) {
      continue;
    }
    diagnostics.push({
      severity: DiagnosticSeverity.Hint,
      range: symbol.range,
      message: unusedDiagnosticMessage(symbol, context.locale),
      source: "asp-lsp-vbscript-unused",
    });
  }
  return diagnostics;
}

function isUnusedDiagnosticCandidate(symbol: VbSymbol): boolean {
  if (symbol.implicit) {
    return false;
  }
  if (symbol.memberOf) {
    return symbol.visibility === "private";
  }
  return ["variable", "parameter", "constant", "function", "sub", "class"].includes(symbol.kind);
}

function unusedDiagnosticMessage(symbol: VbSymbol, locale: AspLocale | undefined): string {
  const localizer = createLocalizer(locale);
  if (symbol.kind === "parameter") {
    return localizer.t("vb.diagnostic.unusedParameter", { name: symbol.name });
  }
  return localizer.t("vb.diagnostic.unusedSymbol", { name: symbol.name });
}

function diagnoseIdentifierCase(
  parsed: AspParsedDocument,
  symbols: VbSymbol[],
  context: VbProjectContext,
): Diagnostic[] {
  const localizer = createLocalizer(context.locale);
  return symbols
    .filter(
      (symbol) =>
        symbol.sourceUri === parsed.uri &&
        /^[A-Za-z][A-Za-z0-9_]*$/.test(symbol.name) &&
        !symbol.implicit &&
        !isRuntimeEntryPoint(parsed, symbol) &&
        !isBuiltinName(symbol.name),
    )
    .flatMap((symbol): Diagnostic[] => {
      const style = identifierCaseForSymbol(symbol, context);
      if (style === "ignore") {
        return [];
      }
      const expectedName = formatIdentifierCase(symbol.name, style);
      return expectedName && expectedName !== symbol.name
        ? [
            {
              severity: DiagnosticSeverity.Hint,
              range: symbol.range,
              message: localizer.t("vb.diagnostic.identifierCase", {
                name: symbol.name,
                expectedName,
                style,
              }),
              source: "asp-lsp-vbscript-naming",
              code: "identifierCase",
              data: {
                name: symbol.name,
                expectedName,
                style,
              },
            },
          ]
        : [];
    });
}

function identifierCaseForSymbol(
  symbol: VbSymbol,
  context: VbProjectContext,
): AspVbscriptIdentifierCase {
  const kind = identifierKindForSymbol(symbol);
  return (
    context.identifierCaseByKind?.[kind] ??
    context.identifierCase ??
    defaultIdentifierCaseForKind(kind)
  );
}

function identifierKindForSymbol(symbol: VbSymbol): AspVbscriptIdentifierKind {
  return symbol.kind === "sub" ? "sub" : symbol.kind;
}

function defaultIdentifierCaseForKind(kind: AspVbscriptIdentifierKind): AspVbscriptIdentifierCase {
  return kind === "variable" || kind === "parameter" ? "camelCase" : "PascalCase";
}

function formatIdentifierCase(
  name: string,
  style: Exclude<AspVbscriptIdentifierCase, "ignore">,
): string | undefined {
  const words = identifierWords(name);
  if (words.length === 0) {
    return undefined;
  }
  switch (style) {
    case "UPPERCASE":
      return words.join("").toUpperCase();
    case "lowercase":
      return words.join("").toLowerCase();
    case "camelCase":
      return [words[0]?.toLowerCase(), ...words.slice(1).map(capitalizeWord)].join("");
    case "PascalCase":
      return words.map(capitalizeWord).join("");
    case "snake_case":
      return words.join("_");
    case "UPPER_SNAKE":
      return words.join("_").toUpperCase();
  }
}

function identifierWords(name: string): string[] {
  return name
    .split("_")
    .flatMap((part) => part.match(/[A-Z]+(?=[A-Z][a-z])|[A-Z]?[a-z]+|[A-Z]+|\d+/g) ?? [])
    .map((word) => word.toLowerCase())
    .filter(Boolean);
}

function capitalizeWord(word: string): string {
  return word.length === 0 ? word : `${word[0]?.toUpperCase()}${word.slice(1).toLowerCase()}`;
}

function isRuntimeEntryPoint(parsed: AspParsedDocument, symbol: VbSymbol): boolean {
  if (symbol.memberOf || (symbol.kind !== "sub" && symbol.kind !== "function")) {
    return false;
  }
  const normalizedUri = parsed.uri.toLowerCase();
  if (!normalizedUri.endsWith("/global.asa") && !normalizedUri.endsWith("\\global.asa")) {
    return false;
  }
  return new Set([
    "application_onstart",
    "application_onend",
    "session_onstart",
    "session_onend",
  ]).has(symbol.name.toLowerCase());
}

function getServerScriptText(parsed: AspParsedDocument): string {
  return serverRegions(parsed)
    .map((region) => parsed.text.slice(region.contentStart, region.contentEnd))
    .join("\n");
}

function serverRegions(parsed: AspParsedDocument): AspRegion[] {
  return parsed.regions.filter((region) => region.language === "vbscript");
}

function vbRegionAt(parsed: AspParsedDocument, offset: number): AspRegion | undefined {
  return parsed.regions
    .filter(
      (region) =>
        region.language === "vbscript" &&
        offset >= region.contentStart &&
        offset < region.contentEnd,
    )
    .sort(
      (left, right) =>
        left.contentEnd - left.contentStart - (right.contentEnd - right.contentStart),
    )[0];
}

function symbolToCompletion(
  symbol: VbSymbol,
  locale: AspLocale | undefined = undefined,
): CompletionItem {
  const kind =
    symbol.kind === "variable" || symbol.kind === "parameter"
      ? CompletionItemKind.Variable
      : symbol.kind === "constant"
        ? CompletionItemKind.Constant
        : symbol.kind === "class"
          ? CompletionItemKind.Class
          : symbol.kind === "field"
            ? CompletionItemKind.Field
            : symbol.kind === "property"
              ? CompletionItemKind.Property
              : CompletionItemKind.Function;
  const detail = symbol.memberOf
    ? `${symbol.kind}${createLocalizer(locale).t("vb.symbol.owner", { owner: symbol.memberOf })}`
    : symbol.typeName
      ? `${symbol.kind} As ${symbol.typeName}`
      : symbol.kind;
  return {
    label: symbol.name,
    kind,
    detail,
    documentation: documentationMarkdown(symbol.documentation, locale),
  };
}

function dedupeCompletions(items: CompletionItem[]): CompletionItem[] {
  const seen = new Set<string>();
  return items.filter((item) => {
    const key = item.label.toLowerCase();
    if (seen.has(key)) {
      return false;
    }
    seen.add(key);
    return true;
  });
}

function significantTokens(parsed: AspParsedDocument): VbToken[] {
  return vbDocuments(parsed)
    .flatMap((document) => document.tokens)
    .filter((token) => !isTriviaToken(token));
}

function identifierTokens(parsed: AspParsedDocument): VbToken[] {
  return significantTokens(parsed).filter((token) => token.kind === "identifier");
}

function identifierTokenAt(parsed: AspParsedDocument, offset: number): VbToken | undefined {
  return identifierTokens(parsed).find((token) => offset >= token.start && offset <= token.end);
}

function previousSignificantToken(parsed: AspParsedDocument, offset: number): VbToken | undefined {
  return significantTokens(parsed)
    .filter((token) => token.end <= offset)
    .at(-1);
}

function nextSignificantToken(parsed: AspParsedDocument, offset: number): VbToken | undefined {
  return significantTokens(parsed).find((token) => token.start >= offset);
}

function isDeclarationNameToken(parsed: AspParsedDocument, token: VbToken): boolean {
  return vbDocuments(parsed)
    .flatMap((document) => flattenVbNodes(document))
    .some(
      (node) =>
        ((node.kind === "Class" || node.kind === "Procedure" || node.kind === "Property") &&
          node.nameToken === token) ||
        node.parameters?.includes(token) ||
        node.identifiers?.includes(token),
    );
}

function callExpressionAt(
  parsed: AspParsedDocument,
  offset: number,
): { name: string; argumentsStart: number } | undefined {
  const tokens = significantTokens(parsed).filter((token) => token.start < offset);
  let depth = 0;
  for (let index = tokens.length - 1; index >= 0; index -= 1) {
    const current = tokens[index];
    if (current.text === ")") {
      depth += 1;
      continue;
    }
    if (current.text !== "(") {
      continue;
    }
    if (depth > 0) {
      depth -= 1;
      continue;
    }
    const name = callNameBefore(tokens, index);
    return name ? { name, argumentsStart: current.end } : undefined;
  }
  return statementCallExpressionAt(parsed, offset);
}

function callNameBefore(tokens: VbToken[], openParenIndex: number): string | undefined {
  const before = tokens[openParenIndex - 1];
  if (!before || before.kind !== "identifier") {
    return undefined;
  }
  if (
    tokens[openParenIndex - 2]?.text === "." &&
    tokens[openParenIndex - 3]?.kind === "identifier"
  ) {
    return `${tokens[openParenIndex - 3].text}.${before.text}`;
  }
  return before.text;
}

function statementCallExpressionAt(
  parsed: AspParsedDocument,
  offset: number,
): { name: string; argumentsStart: number } | undefined {
  const statement = vbStatements(parsed).find((candidate) =>
    statementContainsOffset(candidate, parsed.text, offset),
  );
  if (!statement || statement.some((token) => token.text === "=")) {
    return undefined;
  }
  const nameStartIndex = lowerToken(statement[0]) === "call" ? 1 : 0;
  if (nameStartIndex === 0 && isNonCallStatementStart(statement[0])) {
    return undefined;
  }
  const nameToken = statement[nameStartIndex];
  if (nameToken?.kind !== "identifier") {
    return undefined;
  }
  const name =
    statement[nameStartIndex + 1]?.text === "." &&
    statement[nameStartIndex + 2]?.kind === "identifier"
      ? `${nameToken.text}.${statement[nameStartIndex + 2].text}`
      : nameToken.text;
  const nameEndIndex = name.includes(".") ? nameStartIndex + 2 : nameStartIndex;
  if (statement[nameEndIndex + 1]?.text === "(" || offset <= statement[nameEndIndex].end) {
    return undefined;
  }
  return {
    name,
    argumentsStart: statement[nameEndIndex + 1]?.start ?? statement[nameEndIndex].end,
  };
}

function statementContainsOffset(
  statement: VbToken[],
  sourceText: string,
  offset: number,
): boolean {
  const end = statement.at(-1)?.end ?? statement[0].end;
  if (offset >= statement[0].start && offset <= end) {
    return true;
  }
  return offset > end && /^[ \t]*$/.test(sourceText.slice(end, offset));
}

function isNonCallStatementStart(token: VbToken): boolean {
  return [
    "class",
    "const",
    "dim",
    "do",
    "else",
    "elseif",
    "end",
    "exit",
    "for",
    "function",
    "if",
    "loop",
    "next",
    "option",
    "private",
    "property",
    "public",
    "redim",
    "select",
    "set",
    "sub",
    "wend",
    "while",
    "with",
  ].includes(token.text.toLowerCase());
}

function countActiveParameter(parsed: AspParsedDocument, start: number, offset: number): number {
  const tokens = significantTokens(parsed).filter(
    (token) => token.start >= start && token.end <= offset,
  );
  let depth = 0;
  let count = 0;
  for (const token of tokens) {
    if (token.text === "(") {
      depth += 1;
    } else if (token.text === ")" && depth > 0) {
      depth -= 1;
    } else if (token.text === "," && depth === 0) {
      count += 1;
    }
  }
  return count;
}

function signatureSymbolsForCall(
  parsed: AspParsedDocument,
  name: string,
  offset: number,
  symbols: VbSymbol[],
): VbSymbol[] {
  const [owner, member] = name.includes(".") ? name.split(".", 2) : [undefined, name];
  if (owner && member) {
    const type =
      owner.toLowerCase() === "me"
        ? currentClassTypeRef(parsed, offset, symbols)
        : inferVariableTypeRef(owner, parsed, offset, symbols);
    if (!type) {
      return [];
    }
    const symbol = resolveMemberSymbolForType(type, member, symbols);
    return symbol && (symbol.kind === "method" || symbol.kind === "property") ? [symbol] : [];
  }
  return visibleSymbols(parsed, offset, symbols).filter(
    (symbol) =>
      symbol.name.toLowerCase() === name.toLowerCase() &&
      (symbol.kind === "function" || symbol.kind === "sub"),
  );
}

function typeSignatureLabelsForCall(
  parsed: AspParsedDocument,
  name: string,
  offset: number,
  symbols: VbSymbol[],
  env: VbTypeEnvironment,
): string[] | undefined {
  const [owner, member] = name.includes(".") ? name.split(".", 2) : [undefined, name];
  if (owner && member) {
    const type =
      owner.toLowerCase() === "me"
        ? currentClassTypeRef(parsed, offset, symbols)
        : inferVariableTypeRef(owner, parsed, offset, symbols);
    const signature = type ? memberSignature(type, member, env) : undefined;
    return signature ? [signatureLabelFromMember(owner, member, signature)] : undefined;
  }
  const symbol = visibleSymbols(parsed, offset, symbols).find(
    (candidate) =>
      candidate.name.toLowerCase() === name.toLowerCase() &&
      (candidate.kind === "function" || candidate.kind === "sub"),
  );
  if (!symbol?.type && !symbol?.typeName) {
    return undefined;
  }
  return [signatureLabel(symbol)];
}

function builtinSignatureInformationForCall(
  parsed: AspParsedDocument,
  name: string,
  offset: number,
  symbols: VbSymbol[],
  env: VbTypeEnvironment,
  locale: AspLocale | undefined,
) {
  const [owner, memberName] = name.includes(".") ? name.split(".", 2) : [undefined, name];
  if (owner && memberName) {
    const type =
      owner.toLowerCase() === "me"
        ? currentClassTypeRef(parsed, offset, symbols)
        : (inferVariableTypeRef(owner, parsed, offset, symbols) ?? classicAspObjectTypeRef(owner));
    const member = type ? builtinMemberSpecForType(type.name, memberName) : undefined;
    if (!member?.signature) {
      return undefined;
    }
    return [
      builtinSignatureInformation(
        signatureLabelFromBuiltinParameters(owner, member.name, member.parameters ?? []),
        member.documentation,
        member.parameters ?? [],
        locale,
      ),
    ];
  }
  const builtin = builtinFunction(name);
  if (!builtin) {
    return undefined;
  }
  return [
    builtinSignatureInformation(
      signatureLabelFromBuiltinParameters(builtin.label, undefined, builtin.parameters),
      builtin.documentation,
      builtin.parameters,
      locale,
    ),
  ];
}

function builtinSignatureInformation(
  label: string,
  documentationSpec: BuiltinDocumentationSpec | undefined,
  parameters: BuiltinParameterSpec[],
  locale: AspLocale | undefined,
) {
  return {
    label,
    documentation: builtinDocumentationMarkdown(documentationSpec, locale),
    parameters: parameters.map((parameter) => ({
      label: parameterLabelFromBuiltinSpec(parameter),
      documentation: builtinParameterDocumentation(parameter, documentationSpec, locale),
    })),
  };
}

function builtinParameterDocumentation(
  parameter: BuiltinParameterSpec,
  documentationSpec: BuiltinDocumentationSpec | undefined,
  locale: AspLocale | undefined,
): string | undefined {
  return (
    localizedText(documentationSpec?.parameters?.[parameter.name.toLowerCase()], locale) ||
    localizedText(parameter.documentation, locale) ||
    undefined
  );
}

function signatureLabelFromBuiltinParameters(
  ownerOrFunction: string,
  memberName: string | undefined,
  parameters: BuiltinParameterSpec[],
): string {
  const name = memberName ? `${ownerOrFunction}.${memberName}` : ownerOrFunction;
  return `${name}(${parameters.map(parameterLabelFromBuiltinSpec).join(", ")})`;
}

function parameterLabelFromBuiltinSpec(parameter: BuiltinParameterSpec): string {
  const prefix = parameter.optional ? "Optional " : "";
  return parameter.type
    ? `${prefix}${parameter.name} As ${parameter.type}`
    : `${prefix}${parameter.name}`;
}

function builtinMemberSpecForType(
  typeName: string,
  memberName: string,
): BuiltinMemberSpec | undefined {
  return builtinObjectSpecForType(typeName)?.members.find(
    (member) => member.name.toLowerCase() === memberName.toLowerCase(),
  );
}

function builtinObjectSpecForType(typeName: string): BuiltinObjectSpec | undefined {
  const lower = typeName.toLowerCase();
  return [...Object.values(classicAspObjectCatalog), ...Object.values(externalObjectCatalog)].find(
    (objectSpec) => objectSpec.typeName.toLowerCase() === lower,
  );
}

function signatureLabelFromMember(owner: string, name: string, signature: VbSignature): string {
  const parameters = signature.parameters.map((parameter) => {
    const prefix = parameter.mode
      ? `${parameter.optional ? "Optional " : ""}${parameterModeKeyword(parameter.mode)} `
      : "";
    return parameter.type
      ? `${prefix}${parameter.name} As ${formatTypeRef(parameter.type)}`
      : `${prefix}${parameter.name}`;
  });
  return `${owner}.${name}(${parameters.join(", ")})`;
}

function signatureLabel(symbol: VbSymbol): string {
  const keyword = symbol.kind === "sub" || symbol.kind === "method" ? "Sub" : "Function";
  const owner = symbol.memberOf ? `${symbol.memberOf}.` : "";
  const returnType = symbolTypeRef(symbol);
  return `${keyword} ${owner}${symbol.name}(${parameterLabels(symbol).join(", ")})${
    returnType && keyword === "Function" ? ` As ${formatTypeRef(returnType)}` : ""
  }`;
}

function symbolToSignatureInformation(symbol: VbSymbol, locale: AspLocale | undefined) {
  return {
    label: signatureLabel(symbol),
    documentation: documentationMarkdown(symbol.documentation, locale),
    parameters: parameterDetails(symbol).map((parameter) => ({
      label: parameterLabel(parameter),
      documentation: symbol.documentation?.params[parameter.name],
    })),
  };
}

function diagnoseTypeIssues(
  parsed: AspParsedDocument,
  symbols: VbSymbol[],
  env: VbTypeEnvironment,
  locale: AspLocale | undefined,
): Diagnostic[] {
  const diagnostics: Diagnostic[] = [];
  for (const statement of vbStatements(parsed)) {
    diagnostics.push(...diagnoseAssignmentTypes(parsed, statement, symbols, env, locale));
    diagnostics.push(...diagnoseCallTypes(parsed, statement, symbols, env, locale));
    diagnostics.push(...diagnoseMemberAccess(parsed, statement, symbols, env, locale));
  }
  return diagnostics;
}

function diagnoseAssignmentTypes(
  parsed: AspParsedDocument,
  statement: VbToken[],
  symbols: VbSymbol[],
  env: VbTypeEnvironment,
  locale: AspLocale | undefined,
): Diagnostic[] {
  const localizer = createLocalizer(locale);
  const first = lowerToken(statement[0]);
  const isSet = first === "set";
  const targetIndex = isSet ? 1 : 0;
  const target = statement[targetIndex];
  const equalsIndex = statement.findIndex((token) => token.text === "=");
  if (
    !target ||
    target.kind !== "identifier" ||
    equalsIndex === -1 ||
    statement[targetIndex + 1]?.text === "."
  ) {
    return [];
  }
  const lhsType = inferVariableTypeRef(target.text, parsed, target.start, symbols);
  const rhsType = inferExpressionType(
    parsed,
    statement.slice(equalsIndex + 1),
    symbols,
    env,
    target.start,
  );
  const diagnostics: Diagnostic[] = [];
  if (isSet && rhsType && isClearlyScalarType(rhsType)) {
    const rhsTypeName = formatTypeRef(rhsType);
    diagnostics.push(
      typeWarning(
        parsed,
        target.start,
        statement.at(-1)?.end ?? target.end,
        localizer.t("vb.diagnostic.setScalar", { name: target.text, type: rhsTypeName }),
        "setScalar",
        { name: target.text, type: rhsTypeName },
      ),
    );
  }
  if (!isSet && rhsType && isClearlyObjectType(rhsType, env)) {
    diagnostics.push(
      typeWarning(
        parsed,
        target.start,
        statement.at(-1)?.end ?? target.end,
        localizer.t("vb.diagnostic.objectNeedsSet", { name: target.text }),
        "objectNeedsSet",
        { name: target.text, type: formatTypeRef(rhsType) },
      ),
    );
  }
  if (lhsType && rhsType && !isCompatibleType(lhsType, rhsType, env)) {
    const lhsTypeName = formatTypeRef(lhsType);
    const rhsTypeName = formatTypeRef(rhsType);
    diagnostics.push(
      typeWarning(
        parsed,
        target.start,
        statement.at(-1)?.end ?? target.end,
        localizer.t("vb.diagnostic.typeMismatch", {
          name: target.text,
          expected: lhsTypeName,
          actual: rhsTypeName,
        }),
        "typeMismatch",
        { name: target.text, expected: lhsTypeName, actual: rhsTypeName },
      ),
    );
  }
  return diagnostics;
}

function diagnoseCallTypes(
  parsed: AspParsedDocument,
  statement: VbToken[],
  symbols: VbSymbol[],
  env: VbTypeEnvironment,
  locale: AspLocale | undefined,
): Diagnostic[] {
  const diagnostics: Diagnostic[] = [];
  const localizer = createLocalizer(locale);
  for (let index = 0; index < statement.length; index += 1) {
    if (statement[index].text !== "(" || statement[index - 1]?.kind !== "identifier") {
      continue;
    }
    const name = callNameBefore(statement, index);
    if (!name) {
      continue;
    }
    const signature = signatureForCall(parsed, name, statement[index].start, symbols, env);
    if (!signature) {
      const callName = name.split(".").at(-1) ?? name;
      if (!isLikelyDynamicCall(callName)) {
        diagnostics.push(
          typeWarning(
            parsed,
            statement[index - 1].start,
            statement[index - 1].end,
            localizer.t("vb.diagnostic.unknownCall", { name }),
            "unknownCall",
            { name },
          ),
        );
      }
      continue;
    }
    const closeIndex = matchingCloseParen(statement, index);
    const argumentCount = countArguments(
      statement.slice(index + 1, closeIndex === -1 ? undefined : closeIndex),
    );
    if (argumentCount !== signature.parameters.length) {
      diagnostics.push(
        typeWarning(
          parsed,
          statement[index - 1].start,
          statement[index - 1].end,
          localizer.t("vb.diagnostic.argumentCountMismatch", {
            name,
            expected: signature.parameters.length,
            actual: argumentCount,
          }),
          "argumentCountMismatch",
          { name, expected: signature.parameters.length, actual: argumentCount },
        ),
      );
    }
  }
  return diagnostics;
}

function diagnoseMemberAccess(
  parsed: AspParsedDocument,
  statement: VbToken[],
  symbols: VbSymbol[],
  env: VbTypeEnvironment,
  locale: AspLocale | undefined,
): Diagnostic[] {
  const diagnostics: Diagnostic[] = [];
  const localizer = createLocalizer(locale);
  for (let index = 1; index + 1 < statement.length; index += 1) {
    if (statement[index].text !== "." || statement[index + 1]?.kind !== "identifier") {
      continue;
    }
    const owner = statement[index - 1];
    const member = statement[index + 1];
    const ownerType =
      owner.kind === "identifier"
        ? inferVariableTypeRef(owner.text, parsed, owner.start, symbols)
        : undefined;
    if (!ownerType || isLooseType(ownerType)) {
      continue;
    }
    if (!typeHasMember(ownerType, member.text, env)) {
      const ownerTypeName = formatTypeRef(ownerType);
      diagnostics.push(
        typeWarning(
          parsed,
          member.start,
          member.end,
          localizer.t("vb.diagnostic.missingMember", {
            type: ownerTypeName,
            member: member.text,
          }),
          "missingMember",
          { type: ownerTypeName, member: member.text },
        ),
      );
    }
  }
  return diagnostics;
}

function signatureForCall(
  parsed: AspParsedDocument,
  name: string,
  offset: number,
  symbols: VbSymbol[],
  env: VbTypeEnvironment,
): VbSignature | undefined {
  const [owner, member] = name.includes(".") ? name.split(".", 2) : [undefined, name];
  if (owner && member) {
    const type =
      owner.toLowerCase() === "me"
        ? currentClassTypeRef(parsed, offset, symbols)
        : inferVariableTypeRef(owner, parsed, offset, symbols);
    return type ? memberSignature(type, member, env) : undefined;
  }
  const symbol = visibleSymbols(parsed, offset, symbols).find(
    (candidate) =>
      candidate.name.toLowerCase() === name.toLowerCase() &&
      (candidate.kind === "function" || candidate.kind === "sub"),
  );
  if (!symbol) {
    return undefined;
  }
  return {
    parameters: parameterDetails(symbol).map((parameter) => ({
      name: parameter.name,
      mode: parameter.mode,
      optional: parameter.optional,
    })),
    returnType: symbolTypeRef(symbol),
  };
}

function memberSignature(
  type: VbTypeRef,
  memberName: string,
  env: VbTypeEnvironment,
): VbSignature | undefined {
  const signatures = expandUnionType(typeWithoutNothing(type) ?? type).map(
    (candidate) =>
      findType(env, candidate.name)?.members.find(
        (member) => member.name.toLowerCase() === memberName.toLowerCase(),
      )?.signature,
  );
  return signatures.every(Boolean) ? signatures[0] : undefined;
}

function memberType(
  type: VbTypeRef,
  memberName: string,
  env: VbTypeEnvironment,
): VbTypeRef | undefined {
  const types = expandUnionType(typeWithoutNothing(type) ?? type).map(
    (candidate) =>
      findType(env, candidate.name)?.members.find(
        (member) => member.name.toLowerCase() === memberName.toLowerCase(),
      )?.type,
  );
  return types.every(Boolean)
    ? types.reduce<VbTypeRef | undefined>((merged, item) => mergeTypeRefs(merged, item), undefined)
    : undefined;
}

function memberReturnType(
  type: VbTypeRef,
  memberName: string,
  env: VbTypeEnvironment,
): VbTypeRef | undefined {
  const returnTypes = expandUnionType(typeWithoutNothing(type) ?? type).map(
    (candidate) => memberSignature(candidate, memberName, env)?.returnType,
  );
  return returnTypes.every(Boolean)
    ? returnTypes.reduce<VbTypeRef | undefined>(
        (merged, item) => mergeTypeRefs(merged, item),
        undefined,
      )
    : undefined;
}

function typeHasMember(type: VbTypeRef, memberName: string, env: VbTypeEnvironment): boolean {
  return expandUnionType(typeWithoutNothing(type) ?? type).every((candidate) => {
    const resolved = findType(env, candidate.name);
    return resolved
      ? resolved.members.some((member) => member.name.toLowerCase() === memberName.toLowerCase())
      : true;
  });
}

function builtinTypes(): VbType[] {
  const intrinsic: VbType[] = [
    "String",
    "Byte",
    "Integer",
    "Long",
    "Single",
    "Double",
    "Currency",
    "Decimal",
    "Number",
    "Boolean",
    "Date",
    "Empty",
    "Null",
    "Object",
    "Variant",
    "Nothing",
    "Array",
    "Unknown",
    "Error",
  ].map((name) => ({ name, kind: "intrinsic" as const, members: [] }));
  const classicAsp: VbType[] = Object.values(classicAspObjectCatalog).map((objectSpec) => ({
    name: objectSpec.typeName,
    kind: "classicAsp",
    members: objectSpec.members.map((member) => {
      const signature = member.signature
        ? signatureFromLabel(member.signature, member.type ?? "Variant")
        : undefined;
      return {
        name: member.name,
        kind: member.kind,
        type: signature?.returnType ?? typeRef(member.type ?? "Variant"),
        signature,
        documentation: builtinDocumentationMarkdown(member.documentation),
      };
    }),
  }));
  const external: VbType[] = Object.values(externalObjectCatalog).map((objectSpec) => ({
    name: objectSpec.typeName,
    kind: "com",
    members: objectSpec.members.map((member) => ({
      name: member.name,
      kind: member.kind,
      type: typeRef(member.type ?? "Variant"),
      signature: member.signature
        ? signatureFromLabel(member.signature, member.type ?? "Variant")
        : undefined,
      documentation: builtinDocumentationMarkdown(member.documentation),
    })),
  }));
  return [...intrinsic, ...classicAsp, ...external];
}

function configuredComTypes(comTypes: Record<string, AspVbscriptComType>): VbType[] {
  return Object.entries(comTypes).map(([name, config]) => ({
    name,
    kind: "com",
    members: Object.entries(config.members ?? {}).map(([memberName, member]) => {
      if (typeof member === "string") {
        return { name: memberName, kind: "property", type: typeRef(member) };
      }
      const returnType = member.returnType ?? member.type ?? "Variant";
      return {
        name: memberName,
        kind: member.kind ?? (member.parameters ? "method" : "property"),
        type: typeRef(returnType),
        signature: member.parameters
          ? {
              parameters: member.parameters.map((parameter, index) =>
                typeof parameter === "string"
                  ? { name: `arg${index + 1}`, type: typeRef(parameter) }
                  : {
                      name: parameter.name,
                      type: parameter.type ? typeRef(parameter.type) : undefined,
                    },
              ),
              returnType: typeRef(returnType),
            }
          : undefined,
      };
    }),
  }));
}

function builtinSignature(name: string): VbSignature | undefined {
  const label = builtinSignatureLabels(name)?.[0];
  if (!label) {
    return undefined;
  }
  return signatureFromLabel(label, builtinReturnType(name));
}

function signatureFromLabel(label: string, returnType: string): VbSignature {
  const parameters = parametersFromSignature(label).map((parameter) => ({
    name: parameter.name,
    type: parameter.type ? typeRef(parameter.type) : undefined,
    optional: parameter.optional,
    documentation: localizedText(
      parameter.documentation ?? genericParameterDocumentation(parameter.name),
      undefined,
    ),
  }));
  return { parameters, returnType: typeRef(returnType) };
}

function builtinSignatureLabels(name: string): string[] | undefined {
  const lower = name.toLowerCase();
  const classicAsp = classicAspBuiltinSignatures[lower];
  if (classicAsp) {
    return classicAsp;
  }
  const builtin = builtinFunction(lower);
  return builtin ? [builtin.signature] : undefined;
}

function builtinReturnType(name: string): string {
  const lower = name.toLowerCase();
  const builtin = builtinFunction(lower);
  if (builtin) {
    return builtin.returnType;
  }
  const [ownerName, memberName] = lower.split(".", 2);
  const member = ownerName
    ? classicAspObjectCatalog[ownerName]?.members.find(
        (item) => item.name.toLowerCase() === memberName,
      )
    : undefined;
  if (member?.type) {
    return member.type;
  }
  return "Variant";
}

function builtinFunction(name: string): BuiltinFunction | undefined {
  return builtinFunctions.find((item) => item.label.toLowerCase() === name.toLowerCase());
}

function builtinConstant(name: string): BuiltinConstant | undefined {
  return builtinConstants.find((item) => item.label.toLowerCase() === name.toLowerCase());
}

function typeRef(name: string): VbTypeRef {
  return parseTypeRef(name);
}

export function parseVbscriptTypeRef(text: string): VbTypeRef {
  return parseTypeRef(text);
}

function parseTypeRef(text: string): VbTypeRef {
  const parts = text
    .split("|")
    .map((part) => part.trim())
    .filter(Boolean);
  if (parts.length > 1) {
    return unionTypeRef(parts.map(singleTypeRef));
  }
  return singleTypeRef(parts[0] ?? "Variant");
}

function singleTypeRef(name: string): VbTypeRef {
  const normalized = name.trim() || "Variant";
  return { name: normalized, object: isObjectTypeName(normalized) };
}

function unionTypeRef(types: VbTypeRef[]): VbTypeRef {
  const flattened = types.flatMap((type) => type.unionTypes ?? [type]);
  const unique = new Map<string, VbTypeRef>();
  for (const type of flattened) {
    unique.set(formatTypeRef(type).toLowerCase(), type);
  }
  const unionTypes = [...unique.values()];
  if (unionTypes.length === 0) {
    return typeRef("Variant");
  }
  if (unionTypes.length === 1) {
    return unionTypes[0];
  }
  const name = unionTypes.map(formatTypeRef).join(" | ");
  return {
    name,
    object: unionTypes.every((type) => type.object === true),
    unionTypes,
  };
}

function formatTypeRef(type: VbTypeRef): string {
  return type.unionTypes ? type.unionTypes.map(formatTypeRef).join(" | ") : type.name;
}

function expandUnionType(type: VbTypeRef): VbTypeRef[] {
  return type.unionTypes ?? [type];
}

function typeWithoutNothing(type: VbTypeRef): VbTypeRef | undefined {
  const types = expandUnionType(type).filter((item) => item.name.toLowerCase() !== "nothing");
  return types.length === 0 ? undefined : unionTypeRef(types);
}

function mergeTypeRefs(
  left: VbTypeRef | undefined,
  right: VbTypeRef | undefined,
): VbTypeRef | undefined {
  if (!left) {
    return right;
  }
  if (!right) {
    return left;
  }
  if (formatTypeRef(left).toLowerCase() === formatTypeRef(right).toLowerCase()) {
    return left;
  }
  return unionTypeRef([left, right]);
}

function symbolTypeRef(symbol: VbSymbol | undefined): VbTypeRef | undefined {
  return symbol?.type ?? (symbol?.typeName ? typeRef(symbol.typeName) : undefined);
}

function setSymbolType(symbol: VbSymbol, typeName: string, explicitType = false): void {
  setSymbolTypeRef(symbol, typeRef(typeName), explicitType);
}

function setSymbolTypeRef(symbol: VbSymbol, type: VbTypeRef, explicitType = false): void {
  symbol.type = type;
  symbol.typeName = formatTypeRef(type);
  symbol.explicitType ||= explicitType;
}

function addType(typeMap: Map<string, VbType>, type: VbType): void {
  typeMap.set(type.name.toLowerCase(), type);
}

function findType(env: VbTypeEnvironment, name: string): VbType | undefined {
  return env.types.find((type) => type.name.toLowerCase() === name.toLowerCase());
}

function canonicalBuiltinTypeName(name: string): string {
  return name.charAt(0).toUpperCase() + name.slice(1);
}

function classicAspObjectTypeRef(name: string): VbTypeRef | undefined {
  const objectSpec = classicAspObjectCatalog[name.toLowerCase()];
  return objectSpec ? typeRef(objectSpec.typeName) : undefined;
}

function isObjectTypeName(name: string): boolean {
  const lower = name.toLowerCase();
  return (
    lower === "object" ||
    classicAspTypeNames.has(lower) ||
    (!intrinsicTypeNames.has(lower) &&
      lower !== "string" &&
      lower !== "number" &&
      lower !== "boolean" &&
      lower !== "date")
  );
}

function isLooseType(type: string | VbTypeRef): boolean {
  const types = typeof type === "string" ? expandUnionType(typeRef(type)) : expandUnionType(type);
  return types.some((item) => {
    const lower = item.name.toLowerCase();
    return lower === "unknown" || lower === "variant";
  });
}

function isClearlyObjectType(type: VbTypeRef, env: VbTypeEnvironment): boolean {
  if (
    isLooseType(type) ||
    expandUnionType(type).some((item) => item.name.toLowerCase() === "nothing")
  ) {
    return false;
  }
  return expandUnionType(type).every(
    (item) =>
      item.object === true || Boolean(findType(env, item.name) && !isClearlyScalarType(item)),
  );
}

function isClearlyScalarType(type: VbTypeRef): boolean {
  return expandUnionType(type).every((item) =>
    [
      "string",
      "byte",
      "integer",
      "long",
      "single",
      "double",
      "currency",
      "decimal",
      "number",
      "boolean",
      "date",
      "empty",
      "null",
      "error",
    ].includes(item.name.toLowerCase()),
  );
}

function isCompatibleType(left: VbTypeRef, right: VbTypeRef, env: VbTypeEnvironment): boolean {
  if (isLooseType(left) || isLooseType(right)) {
    return true;
  }
  return expandUnionType(right).every((rightType) =>
    expandUnionType(left).some((leftType) => isCompatibleSingleType(leftType, rightType, env)),
  );
}

function isCompatibleSingleType(
  left: VbTypeRef,
  right: VbTypeRef,
  env: VbTypeEnvironment,
): boolean {
  if (right.name.toLowerCase() === "nothing") {
    return true;
  }
  if (left.name.toLowerCase() === right.name.toLowerCase()) {
    return true;
  }
  if (isNumericTypeName(left.name) && isNumericTypeName(right.name)) {
    return true;
  }
  if (left.name.toLowerCase() === "object" && isClearlyObjectType(right, env)) {
    return true;
  }
  return false;
}

function isNumericTypeName(name: string): boolean {
  return ["byte", "integer", "long", "single", "double", "currency", "decimal", "number"].includes(
    name.toLowerCase(),
  );
}

function typeWarning(
  parsed: AspParsedDocument,
  start: number,
  end: number,
  message: string,
  code: string,
  data?: Record<string, string | number>,
): Diagnostic {
  return {
    severity: DiagnosticSeverity.Warning,
    range: rangeFromOffsets(parsed.text, start, end),
    message,
    code,
    data,
    source: "asp-lsp-vbscript-type",
  };
}

function matchingCloseParen(tokens: VbToken[], openIndex: number): number {
  let depth = 0;
  for (let index = openIndex; index < tokens.length; index += 1) {
    if (tokens[index].text === "(") {
      depth += 1;
    } else if (tokens[index].text === ")") {
      depth -= 1;
      if (depth === 0) {
        return index;
      }
    }
  }
  return -1;
}

function countArguments(tokens: VbToken[]): number {
  const meaningful = tokens.filter((token) => token.text !== ")" && !isTriviaToken(token));
  if (meaningful.length === 0) {
    return 0;
  }
  let depth = 0;
  let count = 1;
  for (const token of meaningful) {
    if (token.text === "(") {
      depth += 1;
    } else if (token.text === ")" && depth > 0) {
      depth -= 1;
    } else if (token.text === "," && depth === 0) {
      count += 1;
    }
  }
  return count;
}

function signatureLabelForDocumentation(symbol: VbSymbol): string {
  if (symbol.kind === "function" || symbol.kind === "sub" || symbol.kind === "method") {
    return signatureLabel(symbol);
  }
  const type = symbolTypeRef(symbol);
  return `${symbol.kind} ${symbol.name}${type ? ` As ${formatTypeRef(type)}` : ""}`;
}

function tokenRangeAt(parsed: AspParsedDocument, offset: number): Range | undefined {
  const token = significantTokens(parsed).find(
    (item) => offset >= item.start && offset <= item.end,
  );
  return token ? rangeFromOffsets(parsed.text, token.start, token.end) : undefined;
}

function statementRangeAt(parsed: AspParsedDocument, offset: number): Range | undefined {
  const statement = vbStatements(parsed).find(
    (tokens) => offset >= (tokens[0]?.start ?? 0) && offset <= (tokens.at(-1)?.end ?? 0),
  );
  return statement
    ? rangeFromOffsets(parsed.text, statement[0].start, statement.at(-1)?.end ?? statement[0].end)
    : undefined;
}

function regionRangeAt(parsed: AspParsedDocument, offset: number): Range | undefined {
  const region = parsed.regions.find(
    (candidate) =>
      (candidate.language === "vbscript" || candidate.language === "jscript") &&
      offset >= candidate.start &&
      offset <= candidate.end,
  );
  return region ? rangeFromOffsets(parsed.text, region.start, region.end) : undefined;
}

function uniqueRanges(ranges: Range[]): Range[] {
  const keys = new Set<string>();
  return ranges
    .filter((range) => {
      const key = `${range.start.line}:${range.start.character}:${range.end.line}:${range.end.character}`;
      if (keys.has(key)) {
        return false;
      }
      keys.add(key);
      return true;
    })
    .sort(
      (left, right) =>
        rangeSize(left) - rangeSize(right) ||
        left.start.line - right.start.line ||
        left.start.character - right.start.character,
    );
}

function buildSelectionRangeChain(ranges: Range[]): SelectionRange {
  let parent: SelectionRange | undefined;
  for (const range of [...ranges].reverse()) {
    parent = { range, parent };
  }
  return (
    parent ?? {
      range: {
        start: { line: 0, character: 0 },
        end: { line: 0, character: 0 },
      },
    }
  );
}

function rangeSize(range: Range): number {
  return (
    (range.end.line - range.start.line) * 100_000 + range.end.character - range.start.character
  );
}

function isRange(value: Range | undefined): value is Range {
  return Boolean(value);
}

function rangeOverlapsOffsets(
  sourceText: string,
  range: Range,
  startOffset: number,
  endOffset: number,
): boolean {
  const start = offsetAt(sourceText, range.start);
  const end = offsetAt(sourceText, range.end);
  return start < endOffset && end > startOffset;
}

function topLevelArgumentStarts(tokens: VbToken[]): VbToken[] {
  const starts: VbToken[] = [];
  let depth = 0;
  let expectingArgument = true;
  for (const token of tokens.filter((item) => !isTriviaToken(item))) {
    if (token.text === "(") {
      depth += 1;
    } else if (token.text === ")" && depth > 0) {
      depth -= 1;
    } else if (token.text === "," && depth === 0) {
      expectingArgument = true;
      continue;
    }
    if (expectingArgument && token.text !== "," && token.text !== ")") {
      starts.push(token);
      expectingArgument = false;
    }
  }
  return starts;
}

function isNamedArgument(statement: VbToken[], token: VbToken): boolean {
  const index = statement.findIndex((item) => item.start === token.start && item.end === token.end);
  return statement[index + 1]?.text === ":=";
}

function typeRefAtOffset(
  parsed: AspParsedDocument,
  offset: number,
  symbols: VbSymbol[],
): VbTypeRef | undefined {
  const member = memberAccessAt(parsed, offset);
  if (member) {
    return member.owner === ""
      ? currentWithTypeRef(parsed, offset, symbols)
      : member.owner.toLowerCase() === "me"
        ? currentClassTypeRef(parsed, offset, symbols)
        : inferVariableTypeRef(member.owner, parsed, offset, symbols);
  }
  const token = identifierTokenAt(parsed, offset);
  return token ? inferVariableTypeRef(token.text, parsed, offset, symbols) : undefined;
}

function isCallableHierarchySymbol(symbol: VbSymbol): boolean {
  return ["function", "sub", "method", "property", "class"].includes(symbol.kind);
}

function symbolToCallHierarchyItem(
  symbol: VbSymbol,
  rootUri = symbol.sourceUri,
): CallHierarchyItem {
  const type = symbolTypeRef(symbol);
  const data: VbCallHierarchyData = {
    uri: symbol.sourceUri,
    name: symbol.name,
    kind: symbol.kind,
    memberOf: symbol.memberOf,
    rootUri,
    line: symbol.range.start.line,
    character: symbol.range.start.character,
  };
  return {
    name: symbol.memberOf ? `${symbol.memberOf}.${symbol.name}` : symbol.name,
    kind: vbCallHierarchySymbolKind(symbol.kind),
    detail: type ? `As ${formatTypeRef(type)}` : symbol.kind,
    uri: symbol.sourceUri,
    range: symbol.scopeRange ?? symbol.range,
    selectionRange: symbol.range,
    data,
  };
}

function vbCallHierarchySymbolKind(kind: VbSymbolKind): SymbolKind {
  if (kind === "class") {
    return SymbolKind.Class;
  }
  if (kind === "method" || kind === "sub") {
    return SymbolKind.Method;
  }
  if (kind === "property" || kind === "field") {
    return SymbolKind.Property;
  }
  return SymbolKind.Function;
}

function callHierarchyTargetSymbol(
  item: CallHierarchyItem,
  symbols: VbSymbol[],
): VbSymbol | undefined {
  const data = item.data as Partial<VbCallHierarchyData> | undefined;
  return symbols.find(
    (symbol) =>
      symbol.sourceUri === (data?.uri ?? item.uri) &&
      symbol.name.toLowerCase() ===
        (data?.name ?? item.name.split(".").at(-1) ?? "").toLowerCase() &&
      symbol.kind === (data?.kind ?? symbol.kind) &&
      (symbol.memberOf ?? "").toLowerCase() === (data?.memberOf ?? "").toLowerCase() &&
      symbol.range.start.line === (data?.line ?? item.selectionRange.start.line) &&
      symbol.range.start.character === (data?.character ?? item.selectionRange.start.character),
  );
}

function callHierarchyRootUri(item: CallHierarchyItem): string {
  const data = item.data as Partial<VbCallHierarchyData> | undefined;
  return data?.rootUri ?? item.uri;
}

function callSitesInDocument(
  parsed: AspParsedDocument,
): Array<{ name: string; offset: number; range: Range }> {
  const calls: Array<{ name: string; offset: number; range: Range }> = [];
  for (const statement of vbStatements(parsed)) {
    for (let index = 0; index < statement.length; index += 1) {
      if (statement[index].text !== "(" || statement[index - 1]?.kind !== "identifier") {
        continue;
      }
      const name = callNameBefore(statement, index);
      if (!name) {
        continue;
      }
      const start = name.includes(".")
        ? (statement[index - 3]?.start ?? statement[index - 1].start)
        : statement[index - 1].start;
      calls.push({
        name,
        offset: statement[index].start,
        range: rangeFromOffsets(parsed.text, start, statement[index - 1].end),
      });
    }
  }
  return calls;
}

function resolveCallTargetSymbol(
  parsed: AspParsedDocument,
  name: string,
  offset: number,
  symbols: VbSymbol[],
): VbSymbol | undefined {
  const [owner, member] = name.includes(".") ? name.split(".", 2) : [undefined, name];
  if (owner && member) {
    const type =
      owner.toLowerCase() === "me"
        ? currentClassTypeRef(parsed, offset, symbols)
        : inferVariableTypeRef(owner, parsed, offset, symbols);
    if (!type) {
      return undefined;
    }
    const symbol = resolveMemberSymbolForType(type, member, symbols);
    return symbol && isCallableHierarchySymbol(symbol) ? symbol : undefined;
  }
  return visibleSymbols(parsed, offset, symbols).find(
    (symbol) =>
      symbol.name.toLowerCase() === name.toLowerCase() && isCallableHierarchySymbol(symbol),
  );
}

function enclosingCallableSymbol(
  parsed: AspParsedDocument,
  offset: number,
  symbols: VbSymbol[],
): VbSymbol | undefined {
  return symbols
    .filter(
      (symbol) =>
        symbol.sourceUri === parsed.uri &&
        isCallableHierarchySymbol(symbol) &&
        rangeContainsOffset(parsed.text, symbol.scopeRange, offset),
    )
    .sort(
      (left, right) =>
        rangeSize(left.scopeRange ?? left.range) - rangeSize(right.scopeRange ?? right.range),
    )[0];
}

function symbolKey(symbol: VbSymbol): string {
  return [
    symbol.sourceUri,
    symbol.kind,
    symbol.memberOf ?? "",
    symbol.name.toLowerCase(),
    symbol.range.start.line,
    symbol.range.start.character,
  ].join("|");
}

function isLikelyDynamicCall(name: string): boolean {
  return /^[A-Z]/.test(name);
}

function isBuiltinName(name: string): boolean {
  const lower = name.toLowerCase();
  return builtinCompletions(undefined).some((item) => item.label.toLowerCase() === lower);
}

function isClassicAspObjectName(name: string): boolean {
  return ["request", "response", "session", "application", "server", "asperror"].includes(
    name.toLowerCase(),
  );
}

function sameSymbol(left: VbSymbol, right: VbSymbol): boolean {
  return (
    left.sourceUri === right.sourceUri &&
    left.name.toLowerCase() === right.name.toLowerCase() &&
    left.kind === right.kind &&
    (left.memberOf ?? "").toLowerCase() === (right.memberOf ?? "").toLowerCase() &&
    left.range.start.line === right.range.start.line &&
    left.range.start.character === right.range.start.character &&
    left.range.end.line === right.range.end.line &&
    left.range.end.character === right.range.end.character
  );
}

function sameRange(left: Range, right: Range): boolean {
  return (
    left.start.line === right.start.line &&
    left.start.character === right.start.character &&
    left.end.line === right.end.line &&
    left.end.character === right.end.character
  );
}

function rangeContainsOffset(
  sourceText: string,
  range: Range | undefined,
  offset: number,
): boolean {
  if (!range) {
    return false;
  }
  const start = offsetAt(sourceText, range.start);
  const end = offsetAt(sourceText, range.end);
  return offset >= start && offset <= end;
}

function symbolPriority(symbol: VbSymbol): number {
  if (symbol.scopeName) {
    return 3;
  }
  if (symbol.memberOf) {
    return 2;
  }
  return 1;
}

function findLastIndex<T>(items: T[], predicate: (item: T) => boolean): number {
  for (let index = items.length - 1; index >= 0; index -= 1) {
    if (predicate(items[index])) {
      return index;
    }
  }
  return -1;
}
