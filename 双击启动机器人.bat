@echo off
setlocal
chcp 65001 >nul

set "DIR=%~dp0"
cd /d "%DIR%"

echo 正在启动 sy-feishu-connect 飞书机器人...
echo.

where sy-feishu-connect >nul 2>nul
if errorlevel 1 (
  echo 没有找到 sy-feishu-connect 命令。
  echo 请先在命令行运行：npm install -g https://github.com/snake-mustang/sy-feishu-connect/archive/refs/heads/main.tar.gz
  echo.
  pause
  exit /b 1
)

set "CONFIG=%USERPROFILE%\.sy-feishu-connect\config.toml"
if not exist "%CONFIG%" (
  echo 没有找到配置文件：%CONFIG%
  echo 请先运行：sy-feishu-connect setup
  echo 或者双击：双击打开配置工具.bat
  echo.
  pause
  exit /b 1
)

echo 启动成功后，请不要关闭这个窗口。
echo 关闭窗口或按 Ctrl+C，机器人就会停止。
echo.
echo 配置文件：%CONFIG%
echo 启动命令：sy-feishu-connect start
echo.

sy-feishu-connect start

echo.
echo 机器人已停止。
pause
