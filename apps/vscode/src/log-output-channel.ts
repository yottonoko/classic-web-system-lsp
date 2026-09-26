import { format } from "node:util";

import * as vscode from "vscode";

/** Creates a log output channel while preserving a custom output language. */
export function createLanguageLogOutputChannel(
  name: string,
  languageId: string,
): vscode.LogOutputChannel {
  return new LanguageLogOutputChannel(vscode.window.createOutputChannel(name, languageId));
}

class LanguageLogOutputChannel implements vscode.LogOutputChannel {
  readonly onDidChangeLogLevel = vscode.env.onDidChangeLogLevel;

  constructor(private readonly channel: vscode.OutputChannel) {}

  get name(): string {
    return this.channel.name;
  }

  get logLevel(): vscode.LogLevel {
    return vscode.env.logLevel;
  }

  trace(message: string, ...args: unknown[]): void {
    this.log(vscode.LogLevel.Trace, message, args);
  }

  debug(message: string, ...args: unknown[]): void {
    this.log(vscode.LogLevel.Debug, message, args);
  }

  info(message: string, ...args: unknown[]): void {
    this.log(vscode.LogLevel.Info, message, args);
  }

  warn(message: string, ...args: unknown[]): void {
    this.log(vscode.LogLevel.Warning, message, args);
  }

  error(error: string | Error, ...args: unknown[]): void {
    this.log(
      vscode.LogLevel.Error,
      error instanceof Error ? error.stack || error.message : error,
      args,
    );
  }

  append(value: string): void {
    this.channel.append(value);
  }

  appendLine(value: string): void {
    this.channel.appendLine(value);
  }

  replace(value: string): void {
    this.channel.replace(value);
  }

  clear(): void {
    this.channel.clear();
  }

  show(preserveFocus?: boolean): void;
  show(column?: vscode.ViewColumn, preserveFocus?: boolean): void;
  show(columnOrPreserveFocus?: vscode.ViewColumn | boolean, preserveFocus?: boolean): void {
    if (typeof columnOrPreserveFocus === "boolean" || columnOrPreserveFocus === undefined) {
      this.channel.show(columnOrPreserveFocus);
      return;
    }
    this.channel.show(columnOrPreserveFocus, preserveFocus);
  }

  hide(): void {
    this.channel.hide();
  }

  dispose(): void {
    this.channel.dispose();
  }

  private log(level: vscode.LogLevel, message: string, args: unknown[]): void {
    if (this.logLevel === vscode.LogLevel.Off || level < this.logLevel) {
      return;
    }
    this.channel.appendLine(format(message, ...args));
  }
}
