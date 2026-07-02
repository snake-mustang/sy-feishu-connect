"use strict";

const childProcess = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");

const rootDir = path.resolve(__dirname, "..");
const nativeName = process.platform === "win32" ? "sy-feishu-codex.exe" : "sy-feishu-codex";
const nativePath = path.join(rootDir, "native", nativeName);

function main() {
  const go = findCommand("go");
  if (!go) {
    console.log("[sy-feishu-connect] 未找到 Go，跳过核心程序编译。稍后可安装 Go 后运行 sy-feishu-connect doctor。");
    return;
  }
  fs.mkdirSync(path.dirname(nativePath), { recursive: true });
  const result = childProcess.spawnSync(go, ["build", "-o", nativePath, "./cmd/sy-feishu-codex"], {
    cwd: rootDir,
    stdio: "inherit",
  });
  if (result.status === 0) {
    console.log("[sy-feishu-connect] 核心程序已编译完成。");
  } else {
    console.log("[sy-feishu-connect] 核心程序编译失败。稍后可运行 sy-feishu-connect doctor 查看原因。");
  }
}

function findCommand(command) {
  const result = childProcess.spawnSync(process.platform === "win32" ? "where" : "which", [command], {
    encoding: "utf8",
  });
  if (result.status !== 0) return "";
  return result.stdout.trim().split(/\r?\n/)[0];
}

main();
