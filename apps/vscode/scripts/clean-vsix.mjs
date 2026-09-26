import fs from "node:fs";
import path from "node:path";

const extensionRoot = path.resolve(import.meta.dirname, "..");

for (const entry of fs.readdirSync(extensionRoot)) {
  if (/^classic-asp-lsp-.*\.vsix$/.test(entry)) {
    fs.rmSync(path.join(extensionRoot, entry), { force: true });
  }
}
