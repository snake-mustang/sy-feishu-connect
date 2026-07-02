#!/bin/bash
export TK_SILENCE_DEPRECATION=1
DIR="$(cd "$(dirname "$0")" && pwd)"
echo "正在启动 sy-feishu-connect 小白配置向导..."
echo "稍后会自动打开浏览器配置页面。"
echo "配置完成前请不要关闭这个终端窗口。"
/usr/bin/env python3 "$DIR/setup-gui.py"
