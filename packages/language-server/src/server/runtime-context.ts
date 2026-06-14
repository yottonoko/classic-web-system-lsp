import type { Connection, TextDocuments } from "vscode-languageserver/node";
import type { TextDocument } from "vscode-languageserver-textdocument";

export interface ServerRuntimeContext {
  connection: Connection;
  documents: TextDocuments<TextDocument>;
}
