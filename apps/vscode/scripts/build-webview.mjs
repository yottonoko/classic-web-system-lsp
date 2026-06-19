import path from "node:path";
import { createRequire } from "node:module";
import react from "@vitejs/plugin-react";
import { build } from "vite";

const extensionRoot = path.resolve(import.meta.dirname, "..");
const require = createRequire(import.meta.url);
const codeHikePackage = require.resolve("codehike/package.json", {
  paths: [extensionRoot],
});
const codeHikeLighterEntry = path.join(
  path.dirname(codeHikePackage),
  "..",
  "@code-hike",
  "lighter",
  "dist",
  "browser.esm.mjs",
);
const tailwindcss = await loadTailwindPlugin();

await buildWebview("include-graph.tsx", "AspLspGraphWebview", "include-graph.js", true);
await buildWebview("flowchart.tsx", "AspLspFlowchartWebview", "flowchart.js", false);
await buildWebview(
  "workspace-files.tsx",
  "AspLspWorkspaceFilesWebview",
  "workspace-files.js",
  false,
);
await buildWebview("settings.tsx", "AspLspSettingsWebview", "settings.js", false);

async function buildWebview(entry, name, fileName, emptyOutDir) {
  await build({
    root: extensionRoot,
    configFile: false,
    define: {
      "process.env.NODE_ENV": JSON.stringify("production"),
    },
    resolve: {
      alias: {
        "@code-hike/lighter": codeHikeLighterEntry,
      },
    },
    plugins: [tailwindcss(), react()],
    build: {
      emptyOutDir,
      outDir: path.join(extensionRoot, "dist", "webview"),
      cssCodeSplit: false,
      minify: true,
      sourcemap: false,
      lib: {
        entry: path.join(extensionRoot, "src", "webview", entry),
        name,
        formats: ["iife"],
        fileName: () => fileName,
      },
    },
  });
}

async function loadTailwindPlugin() {
  const previousNoDeprecation = process.noDeprecation;
  // Tailwind 4.3 imports a Node 26-deprecated loader hook; keep the suppression scoped to that import.
  process.noDeprecation = true;
  try {
    return (await import("@tailwindcss/vite")).default;
  } finally {
    process.noDeprecation = previousNoDeprecation;
  }
}
