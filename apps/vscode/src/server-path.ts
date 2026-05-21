import fs from "node:fs";
import path from "node:path";

export interface ExtensionPathResolver {
  asAbsolutePath(relativePath: string): string;
}

export function getServerModulePath(context: ExtensionPathResolver): string {
  const bundled = context.asAbsolutePath(
    path.join("server", "language-server", "dist", "server.js"),
  );
  if (fs.existsSync(bundled)) {
    return bundled;
  }
  return context.asAbsolutePath(
    path.join("node_modules", "@asp-lsp", "language-server", "dist", "server.js"),
  );
}
