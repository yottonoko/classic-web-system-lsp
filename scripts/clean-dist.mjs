import fs from "node:fs";
import path from "node:path";

const target = process.argv[2] ?? "dist";
const targetPath = path.resolve(process.cwd(), target);
fs.rmSync(targetPath, { recursive: true, force: true });
fs.rmSync(path.resolve(process.cwd(), "tsconfig.tsbuildinfo"), { force: true });
