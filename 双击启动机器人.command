#!/bin/bash
export TK_SILENCE_DEPRECATION=1
DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$DIR" || exit 1

echo "正在启动 sy-feishu-connect 飞书机器人..."
echo

if ! command -v sy-feishu-connect >/dev/null 2>&1; then
  echo "没有找到 sy-feishu-connect 命令。"
  echo "请先在终端运行：npm install -g https://github.com/snake-mustang/sy-feishu-connect/archive/refs/heads/main.tar.gz"
  echo
  read -r -p "按回车键退出..."
  exit 1
fi

CONFIG="$HOME/.sy-feishu-connect/config.toml"
if [ ! -f "$CONFIG" ]; then
  echo "没有找到配置文件：$CONFIG"
  echo "请先运行：sy-feishu-connect setup"
  echo "或者双击：双击打开配置工具.command"
  echo
  read -r -p "按回车键退出..."
  exit 1
fi

echo "启动成功后，请不要关闭这个窗口。"
echo "关闭窗口或按 Ctrl+C，机器人就会停止。"
echo
echo "配置文件：$CONFIG"
echo "启动命令：sy-feishu-connect start"
echo

sy-feishu-connect start

echo
echo "机器人已停止。"
read -r -p "按回车键关闭窗口..."
