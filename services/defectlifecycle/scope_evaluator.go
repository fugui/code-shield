package defectlifecycle

import "code-shield/models"

// ScopeCoverageEvaluator 统一定义生命周期对账的覆盖判定契约。
// 该接口基于本次扫描的全量计划文件集（Planned Files）在内存中构建，
// 与数据库稀疏落库行为完全解耦。生命周期状态机推进代码仅通过此接口判定覆盖。
type ScopeCoverageEvaluator interface {
	// IsCovered 精确判断文件是否在本次扫描的计划且成功覆盖范围内
	// 对于全量扫描：所有计划文件中未失败的均视为已覆盖
	// 对于局部扫描：仅扫描计划内的文件视为已覆盖，计划外文件返回 false
	IsCovered(normPath string) bool

	// IsChanged 判断文件本轮是否被 Git Diff 触碰（代码变更）
	IsChanged(normPath string) bool

	// IsFailed 判断文件是否处理失败，返回失败标识及失败原因
	IsFailed(normPath string) (failed bool, reason string)
}

// MemoryScopeEvaluator 基于全量计划文件集构建的内存高性能覆盖判定器。
type MemoryScopeEvaluator struct {
	plannedFiles map[string]bool   // 全量计划扫描文件集合 (所有 Planned Files)
	failedFiles  map[string]string // 扫描失败文件及错误原因
	touchedFiles map[string]bool   // diff_touched=true 的文件
}

// NewMemoryScopeEvaluator 从全量 ScopeEntry 切片装配内存判定器
func NewMemoryScopeEvaluator(fullEntries []models.ScanScopeEntry) *MemoryScopeEvaluator {
	e := &MemoryScopeEvaluator{
		plannedFiles: make(map[string]bool, len(fullEntries)),
		failedFiles:  make(map[string]string),
		touchedFiles: make(map[string]bool),
	}
	for _, entry := range fullEntries {
		e.plannedFiles[entry.NormPath] = true
		if entry.Outcome == ScopeFailed {
			e.failedFiles[entry.NormPath] = entry.FailReason
		}
		if entry.DiffTouched {
			e.touchedFiles[entry.NormPath] = true
		}
	}
	return e
}

// IsCovered 判定文件是否在计划集内且未失败
func (e *MemoryScopeEvaluator) IsCovered(normPath string) bool {
	if e == nil {
		return false
	}
	if _, failed := e.failedFiles[normPath]; failed {
		return false
	}
	return e.plannedFiles[normPath]
}

// IsChanged 判定文件本轮是否被 Diff 触碰修改
func (e *MemoryScopeEvaluator) IsChanged(normPath string) bool {
	if e == nil {
		return false
	}
	return e.touchedFiles[normPath]
}

// IsFailed 判定文件是否处理失败及失败原因
func (e *MemoryScopeEvaluator) IsFailed(normPath string) (bool, string) {
	if e == nil {
		return false, ""
	}
	reason, failed := e.failedFiles[normPath]
	return failed, reason
}
