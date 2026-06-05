import path from "node:path";
import react from "@vitejs/plugin-react";
import { build } from "vite";

const extensionRoot = path.resolve(import.meta.dirname, "..");

await build({
  root: extensionRoot,
  configFile: false,
  plugins: [react()],
  build: {
    emptyOutDir: true,
    outDir: path.join(extensionRoot, "dist", "webview"),
    cssCodeSplit: false,
    lib: {
      entry: path.join(extensionRoot, "src", "webview", "include-graph.tsx"),
      name: "AspLspGraphWebview",
      formats: ["iife"],
      fileName: () => "include-graph.js",
    },
    rollupOptions: {
      output: {
        inlineDynamicImports: true,
      },
    },
  },
});
