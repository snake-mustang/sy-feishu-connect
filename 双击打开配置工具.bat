@echo off
setlocal
chcp 65001 >nul

set "DIR=%~dp0"
set "SCRIPT=%DIR%setup-gui.py"

if not exist "%SCRIPT%" if exist "%DIR%sy-feishu-connect\setup-gui.py" set "SCRIPT=%DIR%sy-feishu-connect\setup-gui.py"
if not exist "%SCRIPT%" if exist "%DIR%..\sy-feishu-connect\setup-gui.py" set "SCRIPT=%DIR%..\sy-feishu-connect\setup-gui.py"

if not exist "%SCRIPT%" (
  echo 没有找到 setup-gui.py，所以配置向导无法启动。
  echo.
  echo 正确方式：打开完整的 sy-feishu-connect 文件夹，再双击里面的：双击打开配置工具.bat
  echo.
  echo 本脚本已经尝试查找：
  echo   %DIR%setup-gui.py
  echo   %DIR%sy-feishu-connect\setup-gui.py
  echo   %DIR%..\sy-feishu-connect\setup-gui.py
  echo.
  pause
  exit /b 1
)

for %%I in ("%SCRIPT%") do set "APP_DIR=%%~dpI"
cd /d "%APP_DIR%"

echo 正在启动 sy-feishu-connect 小白配置向导...
echo 稍后会自动打开浏览器配置页面。
echo 配置完成前请不要关闭这个窗口。

py -3 "%SCRIPT%" 2>nul
if errorlevel 1 (
  python "%SCRIPT%"
  if errorlevel 1 (
    echo.
    echo Python 启动失败。请安装 Python 3，或把 Python 加到 PATH。
    echo 下载地址：https://www.python.org/downloads/windows/
    pause
  )
)
