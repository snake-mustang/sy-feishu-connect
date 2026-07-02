@echo off
setlocal
chcp 65001 >nul

set "DIR=%~dp0"
cd /d "%DIR%"

echo 正在启动 sy-feishu-connect 飞书机器人...
echo.

if not exist "%DIR%config.toml" (
  echo 没有找到 config.toml。
  echo 请先双击：双击打开配置工具.bat
  echo.
  pause
  exit /b 1
)

set "BIN=%DIR%bin\sy-feishu-codex.exe"
if not exist "%BIN%" (
  echo 没有找到可执行文件：bin\sy-feishu-codex.exe
  echo 正在尝试自动编译...
  if not exist "%DIR%bin" mkdir "%DIR%bin"
  go build -o "%BIN%" ./cmd/sy-feishu-codex
  if errorlevel 1 (
    echo.
    echo 编译失败。请先确认已经安装 Go，并且终端可以运行 go version。
    echo.
    pause
    exit /b 1
  )
)

echo 启动成功后，请不要关闭这个窗口。
echo 关闭窗口或按 Ctrl+C，机器人就会停止。
echo.
echo 配置文件：%DIR%config.toml
echo 启动命令：%BIN% -config %DIR%config.toml
echo.

"%BIN%" -config "%DIR%config.toml"

echo.
echo 机器人已停止。
pause
