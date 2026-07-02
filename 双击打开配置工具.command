#!/bin/bash
export TK_SILENCE_DEPRECATION=1
DIR="$(cd "$(dirname "$0")" && pwd)"
SCRIPT="$DIR/setup-gui.py"

if [ ! -f "$SCRIPT" ] && [ -f "$DIR/sy-feishu-connect/setup-gui.py" ]; then
  SCRIPT="$DIR/sy-feishu-connect/setup-gui.py"
fi

if [ ! -f "$SCRIPT" ] && [ -f "$DIR/../sy-feishu-connect/setup-gui.py" ]; then
  SCRIPT="$DIR/../sy-feishu-connect/setup-gui.py"
fi

if [ ! -f "$SCRIPT" ]; then
  echo "没有找到 setup-gui.py，所以配置向导无法启动。"
  echo
  echo "最常见原因：你只双击了一个单独拷出来的 .command 文件。"
  echo "正确方式：打开完整的 sy-feishu-connect 文件夹，再双击里面的：双击打开配置工具.command"
  echo
  echo "本脚本已经尝试查找："
  echo "  $DIR/setup-gui.py"
  echo "  $DIR/sy-feishu-connect/setup-gui.py"
  echo "  $DIR/../sy-feishu-connect/setup-gui.py"
  echo
  read -r -p "按回车键关闭窗口..."
  exit 1
fi

APP_DIR="$(cd "$(dirname "$SCRIPT")" && pwd)"
cd "$APP_DIR" || exit 1

echo "正在启动 sy-feishu-connect 小白配置向导..."
echo "稍后会自动打开浏览器配置页面。"
echo "配置完成前请不要关闭这个终端窗口。"
/usr/bin/env python3 "$SCRIPT"
