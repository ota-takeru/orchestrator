import { spawn } from "node:child_process";
import { mkdirSync, rmSync } from "node:fs";

mkdirSync("test-results", { recursive: true });
for (const suffix of ["", "-shm", "-wal"]) {
  rmSync(`test-results/e2e-registry.sqlite${suffix}`, { force: true });
}

const child = spawn(
  "go",
  [
    "run",
    "../cmd/devos",
    "serve",
    "--project-root",
    "..",
    "--registry",
    "test-results/e2e-registry.sqlite",
    "--ui",
    "--ui-dir",
    "dist",
    "--addr",
    "127.0.0.1:8767"
  ],
  { stdio: "inherit" }
);

function stop() {
  if (!child.killed) {
    child.kill();
  }
}

process.on("SIGINT", stop);
process.on("SIGTERM", stop);
child.on("exit", (code, signal) => {
  if (signal) {
    process.kill(process.pid, signal);
    return;
  }
  process.exit(code ?? 0);
});
