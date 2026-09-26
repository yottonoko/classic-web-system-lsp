import * as vscode from "vscode";
import type {
  DiagnosticCollectionProvider,
  DiagnosticCollectionSource,
} from "vscode-languageclient/node";

/**
 * Shares one VS Code diagnostic collection between the language client's push
 * and pull diagnostic implementations.
 *
 * The language client releases each implementation independently, but those
 * releases belong to an individual client. The extension owns this provider
 * across client restarts, so only the parameterless dispose tears down the
 * collection.
 */
export class SharedDiagnosticCollectionProvider implements DiagnosticCollectionProvider {
  private collection: vscode.DiagnosticCollection | undefined;

  constructor(
    private readonly createCollection: (name: string | undefined) => vscode.DiagnosticCollection = (
      name,
    ) => vscode.languages.createDiagnosticCollection(name),
  ) {}

  create(
    name: string | undefined,
    _source: DiagnosticCollectionSource,
  ): vscode.DiagnosticCollection {
    this.collection ??= this.createCollection(name);
    return this.collection;
  }

  dispose(): void;
  dispose(collection: vscode.DiagnosticCollection, source: DiagnosticCollectionSource): void;
  dispose(collection?: vscode.DiagnosticCollection, _source?: DiagnosticCollectionSource): void {
    if (collection !== undefined || _source !== undefined) {
      return;
    }
    const activeCollection = this.collection;
    this.collection = undefined;
    activeCollection?.dispose();
  }
}
