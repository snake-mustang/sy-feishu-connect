#!/bin/bash
export TK_SILENCE_DEPRECATION=1
DIR="$(cd "$(dirname "$0")" && pwd)"
echo "正在打开 sy-feishu-connect 配置向导..."
echo "如果稍后弹出图形窗口，请在窗口里填写信息并点击开始。"
/usr/bin/env python3 "$DIR/setup-gui.py"
