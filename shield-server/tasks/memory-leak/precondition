#!/bin/bash
# 前置检查：最近 7 天是否有代码提交
# 与 code-review-task 共用同一逻辑
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
