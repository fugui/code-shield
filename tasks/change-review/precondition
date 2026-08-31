#!/bin/bash
# 前置检查：最近 7 天是否有代码提交
# 输入: $1 = 代码仓本地路径
# 退出码: 0=继续, 1=跳过, 2=失败
# 标准输出: 跳过/失败原因

CODES_PATH="$1"

if [ -z "$CODES_PATH" ]; then
    echo "未提供代码路径参数"
    exit 2
fi

if [ ! -d "$CODES_PATH/.git" ]; then
    echo "指定路径不是 git 仓库"
    exit 2
fi

RECENT=$(git -C "$CODES_PATH" log --since="7 days ago" --oneline 2>/dev/null)
if [ -z "$RECENT" ]; then
    echo "过去 7 天内无代码提交，已完成确认，无需执行任务。"
    exit 1
fi

exit 0
