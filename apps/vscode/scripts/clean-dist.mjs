import * as fs from "node:fs";
import * as path from "node:path";

const distDir = path.resolve(import.meta.dirname, "..", "dist");
fs.rmSync(distDir, { recursive: true, force: true });
