#!/bin/bash
# 后置处理：解析内存泄漏检测报告
# 输入: $1 = AI 生成的 Markdown 报告文件路径
# 输出: 标准输出一行 JSON

REPORT_PATH="$1"

if [ -z "$REPORT_PATH" ] || [ ! -f "$REPORT_PATH" ]; then
    echo '{"score":0,"summary":"报告文件不存在","metrics":{}}'
    exit 0
fi

CONTENT=$(cat "$REPORT_PATH")

# 解析 "高风险：N，中风险：N，低风险：N" 格式
HIGH=$(echo "$CONTENT" | grep -oP '高风险[：:]\s*\K\d+' | head -1)
MEDIUM=$(echo "$CONTENT" | grep -oP '中风险[：:]\s*\K\d+' | head -1)
LOW=$(echo "$CONTENT" | grep -oP '低风险[：:]\s*\K\d+' | head -1)

HIGH=${HIGH:-0}
MEDIUM=${MEDIUM:-0}
LOW=${LOW:-0}

SCORE=$(( HIGH*5 + MEDIUM*3 + LOW ))

# 提取摘要
SUMMARY=$(echo "$CONTENT" | sed -n '/^## .*摘要/,/^## /{ /^## .*摘要/d; /^## /d; p; }' | head -10 | tr '\n' ' ' | cut -c1-500)

cat <<EOF
{"score":${SCORE},"summary":"$(echo "$SUMMARY" | sed 's/"/\\"/g')","metrics":{"high_risk":${HIGH},"medium_risk":${MEDIUM},"low_risk":${LOW}}}
EOF
