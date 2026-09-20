import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { parse } from "@vue/compiler-sfc";

const toolsRoot = path.dirname(fileURLToPath(import.meta.url));
const frontendRoot = path.resolve(toolsRoot, "..");
const sourceRoot = path.join(frontendRoot, "src");

function collectVueFiles(directory) {
  const files = [];
  for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
    const entryPath = path.join(directory, entry.name);
    if (entry.isDirectory()) {
      files.push(...collectVueFiles(entryPath));
    } else if (entry.isFile() && entry.name.endsWith(".vue")) {
      files.push(entryPath);
    }
  }
  return files;
}

function formatError(error) {
  if (typeof error === "string") return error;
  const location = error.loc?.start;
  if (!location) return error.message || String(error);
  return `${error.message || String(error)} (line ${location.line}, column ${location.column})`;
}

const vueFiles = collectVueFiles(sourceRoot);
const failures = [];

for (const filePath of vueFiles) {
  const source = fs.readFileSync(filePath, "utf8");
  const result = parse(source, { filename: filePath });
  if (result.errors.length > 0) {
    const relativePath = path.relative(frontendRoot, filePath);
    failures.push(`${relativePath}:\n${result.errors.map((error) => `  - ${formatError(error)}`).join("\n")}`);
  }
}

if (failures.length > 0) {
  console.error("Vue SFC syntax errors detected:");
  console.error(failures.join("\n"));
  process.exitCode = 1;
} else {
  console.log(`Vue SFC syntax check passed (${vueFiles.length} files).`);
}
