package runner

import "errors"

// ErrSkipped 在前置条件未满足（例如代码无增量变更）时返回，通知外层队列优雅跳过本次扫描
var ErrSkipped = errors.New("task skipped by precondition")
