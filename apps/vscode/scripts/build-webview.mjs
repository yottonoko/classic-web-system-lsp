import path from "node:path";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { build } from "vite";

const extensionRoot = path.resolve(import.meta.dirname, "..");

await buildWebview("include-graph.tsx", "AspLspGraphWebview", "include-graph.js", true);
await buildWebview("flowchart.tsx", "AspLspFlowchartWebview", "flowchart.js", false);

async function buildWebview(entry, name, fileName, emptyOutDir) {
  await build({
    root: extensionRoot,
    configFile: false,
    define: {
      "process.env.NODE_ENV": JSON.stringify("production"),
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
      rollupOptions: {
        output: {
          inlineDynamicImports: true,
        },
      },
    },
  });
}
