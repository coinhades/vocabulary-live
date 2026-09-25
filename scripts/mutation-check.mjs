// Intentional experiment: copy only backend source, remove one business guard,
// require its regression test to fail, then rerun the original. Never edit main source.
import { cpSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { createHash } from "node:crypto";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import path from "node:path";
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const source = path.join(root, "backend/internal/store/transition.lua");
const bytes = readFileSync(source);
const hash = (value) => createHash("sha256").update(value).digest("hex");
const destination = path.join(
  root,
  ".cache",
  `mutation-${Date.now()}`,
  "backend",
);
mkdirSync(destination, { recursive: true });
cpSync(path.join(root, "backend"), destination, { recursive: true });
const guard = "if previous then return {'ALREADY_ANSWERED',previous} end";
const text = bytes.toString("utf8");
if (text.split(guard).length !== 2)
  throw new Error("Expected exactly one business-uniqueness guard");
writeFileSync(
  path.join(destination, "internal/store/transition.lua"),
  text.replace(guard, "-- INTENTIONAL MUTATION: question uniqueness disabled"),
);
const args = [
  "test",
  "-race",
  "-count=1",
  "-timeout=60s",
  "-tags",
  "integration",
  "-run",
  "^TestAcceptanceReplayAndUniqueness$",
  "./internal/store",
];
const mutant = spawnSync("go", args, {
  cwd: destination,
  env: process.env,
  encoding: "utf8",
});
const output = `${mutant.stdout ?? ""}${mutant.stderr ?? ""}`;
writeFileSync(
  path.join(root, "docs/evidence/mutation.txt"),
  `Intentional guard-removal experiment\nCommand: go ${args.join(" ")}\n\n${output}`,
);
if (
  mutant.error ||
  mutant.status === 0 ||
  !output.includes("want ALREADY_ANSWERED got <nil>")
)
  throw new Error("Mutation was not detected for the expected reason");
if (hash(readFileSync(source)) !== hash(bytes))
  throw new Error("Original source was changed");
const original = spawnSync("go", args, {
  cwd: path.join(root, "backend"),
  env: process.env,
  encoding: "utf8",
});
writeFileSync(
  path.join(root, "docs/evidence/mutation-restored.txt"),
  `Original source SHA256: ${hash(bytes)}\n${original.stdout ?? ""}${original.stderr ?? ""}`,
);
if (original.error || original.status !== 0)
  throw new Error("Original regression test failed");
console.log(
  "Mutated copy failed for the expected missing-guard assertion; untouched original passed.",
);
