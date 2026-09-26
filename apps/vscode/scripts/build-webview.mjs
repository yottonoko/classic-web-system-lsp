import path from "node:path";
import solid from "@solidjs/vite-plugin";
import { build } from "vite";

const extensionRoot = path.resolve(import.meta.dirname, "..");
const tailwindcss = await loadTailwindPlugin();

await buildWebview("flowchart.tsx", "AspLspFlowchartWebview", "flowchart.js", true);
await buildWebview(
  "navigation-graph.tsx",
  "AspLspNavigationGraphWebview",
  "navigation-graph.js",
  false,
);
await buildWebview(
  "workspace-files.tsx",
  "AspLspWorkspaceFilesWebview",
  "workspace-files.js",
  false,
);

await buildWebview("log-analysis.tsx", "AspLspLogAnalysisWebview", "log-analysis.js", false);

async function buildWebview(entry, name, fileName, emptyOutDir) {
  await build({
    root: extensionRoot,
    configFile: false,
    define: {
      "process.env.NODE_ENV": JSON.stringify("production"),
    },
    plugins: [tailwindcss(), solid()],
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
