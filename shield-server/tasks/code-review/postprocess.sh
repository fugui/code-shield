#!/bin/bash
# 后置处理：解析代码检视报告
# 输入: $1 = AI 生成的 Markdown 报告文件路径
# 输出: 标准输出一行 JSON
# {"score":25,"summary":"...","metrics":{"blocking":0,"critical":2,...}}

REPORT_PATH="$1"

if [ -z "$REPORT_PATH" ] || [ ! -f "$REPORT_PATH" ]; then
    echo '{"score":0,"summary":"报告文件不存在","metrics":{}}'
    exit 0
fi

CONTENT=$(cat "$REPORT_PATH")

# 解析 "阻塞：N，严重：N，主要：N，提示：N，建议：N" 格式
BLOCKING=$(echo "$CONTENT" | grep -oP '阻塞[：:]\s*\K\d+' | head -1)
CRITICAL=$(echo "$CONTENT" | grep -oP '严重[：:]\s*\K\d+' | head -1)
MAJOR=$(echo "$CONTENT" | grep -oP '主要[：:]\s*\K\d+' | head -1)
HINT=$(echo "$CONTENT" | grep -oP '提示[：:]\s*\K\d+' | head -1)
SUGGESTION=$(echo "$CONTENT" | grep -oP '建议[：:]\s*\K\d+' | head -1)

BLOCKING=${BLOCKING:-0}
CRITICAL=${CRITICAL:-0}
MAJOR=${MAJOR:-0}
HINT=${HINT:-0}
SUGGESTION=${SUGGESTION:-0}

# 加权分数
SCORE=$(( BLOCKING*5 + CRITICAL*4 + MAJOR*3 + HINT*2 + SUGGESTION ))

# 提取检视摘要
SUMMARY=$(echo "$CONTENT" | sed -n '/^## 检视摘要/,/^## /{ /^## 检视摘要/d; /^## /d; p; }' | head -10 | tr '\n' ' ' | cut -c1-500)

cat <<EOF
{"score":${SCORE},"summary":"$(echo "$SUMMARY" | sed 's/"/\\"/g')","metrics":{"blocking":${BLOCKING},"critical":${CRITICAL},"major":${MAJOR},"hint":${HINT},"suggestion":${SUGGESTION}}}
EOF
