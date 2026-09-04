package services

import (
	"code-shield/services/defects"
)

// SourceAnchor 物理源码锚点（缺陷的确定性物理身份证，统一复用 defects.SourceAnchor SSOT）
type SourceAnchor = defects.SourceAnchor

// CleanSourceToken 对代码行进行 Token 级去噪清洗 (去除注释、空白符、引号、分号并转小写)
func CleanSourceToken(line string) string {
	return defects.CleanSourceToken(line)
}

// NormalizeScopeSymbol 规范化作用域符号，去除外层命名空间与 lambda 差异
func NormalizeScopeSymbol(rawScope string) string {
	return defects.NormalizeScopeSymbol(rawScope)
}

// LocateTriggerNearby 在 targetLine 前后指定窗口内滑动寻找最匹配 cleanTrigger 的物理行
func LocateTriggerNearby(lines []string, cleanTrigger string, targetLine int, windowSize int) int {
	return defects.LocateTriggerNearby(lines, cleanTrigger, targetLine, windowSize)
}

// LocateTriggerInLines 在整篇文件中模糊反查 cleanTrigger 所在真实行号 (解决行号为空的场景)
func LocateTriggerInLines(lines []string, cleanTrigger string) int {
	return defects.LocateTriggerInLines(lines, cleanTrigger)
}

// ExtractScopeAndBodyFromLines 从目标行向上逆向扫描提取物理函数作用域签名及函数体代码
func ExtractScopeAndBodyFromLines(filePath string, lines []string, targetLine int) (string, string) {
	return defects.ExtractScopeAndBodyFromLines(filePath, lines, targetLine)
}

// ComputeFileSHA256 计算物理文件的 SHA-256 快照哈希
func ComputeFileSHA256(fullPath string) (string, error) {
	return defects.ComputeFileSHA256(fullPath)
}

// ParseLineNumberRange 解析 "10-20" 或 "15" 格式的行号，返回起始行与结束行
func ParseLineNumberRange(rawLine string) (int, int) {
	return defects.ParseLineNumberRange(rawLine)
}

// EnrichSourceAnchor 从磁盘物理源文件中提取确定性特征与物理锚点
func EnrichSourceAnchor(repoRoot string, filePath string, rawLine string, rawTrigger string) (*SourceAnchor, error) {
	return defects.EnrichSourceAnchor(repoRoot, filePath, rawLine, rawTrigger)
}
