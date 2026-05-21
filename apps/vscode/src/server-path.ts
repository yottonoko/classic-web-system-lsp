import path from "node:path";

export interface ExtensionPathResolver {
  asAbsolutePath(relativePath: string): string;
}

export function getServerModulePath(context: ExtensionPathResolver): string {
  return context.asAbsolutePath(
    path.join("node_modules", "@asp-lsp", "language-server", "dist", "server.js"),
  );
}
