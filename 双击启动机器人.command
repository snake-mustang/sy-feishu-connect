#!/bin/bash
export TK_SILENCE_DEPRECATION=1
DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$DIR" || exit 1

echo "正在启动 sy-feishu-connect 飞书机器人..."
echo

if [ ! -f "$DIR/config.toml" ]; then
  echo "没有找到 config.toml。"
  echo "请先双击：双击打开配置工具.command"
  echo
  read -r -p "按回车键退出..."
  exit 1
fi

if [ ! -x "$DIR/bin/sy-feishu-codex" ]; then
  echo "没有找到可执行文件：bin/sy-feishu-codex"
  echo "正在尝试自动编译..."
  if ! make build; then
    echo
    echo "编译失败。请先双击：双击打开配置工具.command"
    echo
    read -r -p "按回车键退出..."
    exit 1
  fi
fi

echo "启动成功后，请不要关闭这个窗口。"
echo "关闭窗口或按 Ctrl+C，机器人就会停止。"
echo
echo "配置文件：$DIR/config.toml"
echo "启动命令：$DIR/bin/sy-feishu-codex -config $DIR/config.toml"
echo

"$DIR/bin/sy-feishu-codex" -config "$DIR/config.toml"

echo
echo "机器人已停止。"
read -r -p "按回车键关闭窗口..."
