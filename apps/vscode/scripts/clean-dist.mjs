import { rm } from "node:fs/promises";
import path from "node:path";

const extensionRoot = path.resolve(import.meta.dirname, "..");

await rm(path.join(extensionRoot, "dist"), { recursive: true, force: true });
