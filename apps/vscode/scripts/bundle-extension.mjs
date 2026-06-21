import { builtinModules } from "node:module";
import { rm } from "node:fs/promises";
import path from "node:path";
import { rolldown } from "rolldown";

const extensionRoot = path.resolve(import.meta.dirname, "..");
const nodeBuiltins = new Set([...builtinModules, ...builtinModules.map((name) => `node:${name}`)]);
const removedSettingsPanelOutputs = [
  "dist/settings-webview.js",
  "dist/settings-webview.d.ts",
  "dist/settings-metadata.js",
  "dist/settings-metadata.d.ts",
  "dist/webview/settings.js",
];

await Promise.all(
  removedSettingsPanelOutputs.map((file) =>
    rm(path.join(extensionRoot, file), { force: true, recursive: true }),
  ),
);

const bundle = await rolldown({
  input: path.join(extensionRoot, "src", "extension.ts"),
  platform: "node",
  external: (id) => id === "vscode" || nodeBuiltins.has(id),
});

try {
  await bundle.write({
    file: path.join(extensionRoot, "dist", "extension.js"),
    format: "cjs",
    sourcemap: false,
    minify: true,
    exports: "named",
  });
} finally {
  await bundle.close();
}
