#!/usr/bin/env node
import { spawn, spawnSync } from "node:child_process";
import { existsSync, mkdirSync, readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const frontend = path.join(root, "frontend");
const backend = path.join(root, "backend");
const exe = process.platform === "win32" ? ".exe" : "";
const npm = "npm";
const env = { ...process.env };
function invocation(command, args) {
  if (process.platform === "win32" && command === npm) {
    return [
      process.execPath,
      [
        path.join(
          path.dirname(process.execPath),
          "node_modules/npm/bin/npm-cli.js",
        ),
        ...args,
      ],
    ];
  }
  return [command, args];
}
function run(command, args, cwd = root, extra = {}) {
  console.log(`> ${command} ${args.join(" ")}`);
  const [executable, parameters] = invocation(command, args);
  const result = spawnSync(executable, parameters, {
    cwd,
    env: { ...env, ...extra },
    stdio: "inherit",
  });
  if (result.error) {
    console.error(result.error.message);
    process.exit(1);
  }
  if (result.status !== 0) process.exit(result.status ?? 1);
}
function background(command, args, cwd, extra = {}) {
  const [executable, parameters] = invocation(command, args);
  const child = spawn(executable, parameters, {
    cwd,
    env: { ...env, ...extra },
    stdio: "inherit",
  });
  child.on("error", (error) => {
    console.error(error);
    process.exitCode = 1;
  });
  return child;
}
function goFiles(dir) {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) =>
    entry.isDirectory()
      ? goFiles(path.join(dir, entry.name))
      : entry.name.endsWith(".go")
        ? [path.join(dir, entry.name)]
        : [],
  );
}
function build() {
  run(npm, ["run", "build"], frontend);
  mkdirSync(path.join(root, "dist"), { recursive: true });
  run(
    "go",
    [
      "build",
      "-trimpath",
      "-o",
      path.join(root, `dist/server${exe}`),
      "./cmd/server",
    ],
    backend,
  );
}
const tasks = {
  build,
  up: () => run("docker", ["compose", "up", "--build"]),
  down: () => run("docker", ["compose", "down"]),
  dev: () => {
    const api = background("go", ["run", "./cmd/server"], backend);
    const ui = background(npm, ["run", "dev"], frontend);
    const stop = () => {
      api.kill();
      ui.kill();
    };
    process.on("SIGINT", stop);
    process.on("SIGTERM", stop);
    api.on("exit", (code) => {
      ui.kill();
      process.exitCode = code ?? 1;
    });
    ui.on("exit", (code) => {
      api.kill();
      process.exitCode = code ?? 1;
    });
  },
  "fmt-check": () => {
    const result = spawnSync("gofmt", ["-l", ...goFiles(backend)], {
      cwd: root,
      encoding: "utf8",
      env,
    });
    if (result.error || result.status !== 0 || result.stdout.trim()) {
      console.error(result.error ?? result.stderr ?? "", result.stdout);
      process.exit(1);
    }
    run(
      npm,
      [
        "exec",
        "--",
        "prettier",
        "--check",
        "src",
        "*.ts",
        "*.mjs",
        "*.json",
        "tests",
      ],
      frontend,
    );
  },
  lint: () => {
    run("go", ["vet", "-tags", "integration", "./..."], backend);
    run(npm, ["run", "lint"], frontend);
  },
  typecheck: () => run(npm, ["run", "typecheck"], frontend),
  "test-unit": () => {
    run("go", ["test", "-race", "./..."], backend);
    run(npm, ["test"], frontend);
  },
  "test-integration": () =>
    run(
      "go",
      ["test", "-race", "-count=1", "-tags", "integration", "./..."],
      backend,
    ),
  "test-e2e": () => {
    if (!existsSync(path.join(root, `dist/server${exe}`))) build();
    run(npm, ["exec", "--", "playwright", "test"], frontend);
    run(
      npm,
      [
        "exec",
        "--",
        "playwright",
        "test",
        "--config",
        "playwright.learning.config.ts",
      ],
      frontend,
    );
  },
  "serve-learning-e2e": () => {
    run(
      "go",
      [
        "build",
        "-o",
        path.join(root, `dist/learning-testserver${exe}`),
        "./cmd/learningtest",
      ],
      backend,
    );
    const server = background(
      path.join(root, `dist/learning-testserver${exe}`),
      [],
      root,
      { OPENAI_API_KEY: "" },
    );
    process.on("SIGTERM", () => server.kill("SIGTERM"));
    process.on("SIGINT", () => server.kill("SIGINT"));
    server.on("exit", (code) => {
      process.exitCode = code ?? 1;
    });
  },
  "serve-e2e": () => {
    const port = env.E2E_PORT ?? "8080";
    const config = {
      OPENAI_API_KEY: "",
      AI_AUTHORING_ENABLED: "false",
      REDIS_NAMESPACE: "vocab-e2e",
      APP_ENV: "development",
      ALLOW_DEMO_RESET: "yes",
      RATE_LIMIT_PER_SECOND: "1000",
      RATE_LIMIT_BURST: "2000",
      STATIC_DIR: path.join(frontend, "dist"),
      HTTP_ADDR: `127.0.0.1:${port}`,
      ALLOWED_ORIGINS: `http://127.0.0.1:${port},http://localhost:${port}`,
    };
    run(path.join(root, `dist/server${exe}`), ["--demo-reset"], root, config);
    const server = background(
      path.join(root, `dist/server${exe}`),
      [],
      root,
      config,
    );
    process.on("SIGTERM", () => server.kill("SIGTERM"));
    process.on("SIGINT", () => server.kill("SIGINT"));
    server.on("exit", (code) => {
      process.exitCode = code ?? 1;
    });
  },
  verify: () => {
    for (const name of [
      "fmt-check",
      "lint",
      "typecheck",
      "test-unit",
      "test-integration",
      "build",
      "test-e2e",
    ])
      tasks[name]();
  },
  "demo-reset": () =>
    run("go", ["run", "./cmd/server", "--demo-reset"], backend, {
      APP_ENV: "development",
      ALLOW_DEMO_RESET: "yes",
    }),
  "load-test": () =>
    run("go", ["run", "./cmd/load", ...process.argv.slice(3)], backend),
};
const name = process.argv[2];
if (!Object.hasOwn(tasks, name)) {
  console.error(`Choose a task: ${Object.keys(tasks).join(", ")}`);
  process.exit(1);
}
tasks[name]();
