package services

import (
	"code-shield/services/engines/chunker"
)

// SemanticBundle 语义感知分片数据包别名
type SemanticBundle = chunker.SemanticBundle

// BuildSemanticBundles 扫描仓库文件并构建语义感知分片，委托给 chunker 子包
func BuildSemanticBundles(codesPath string, cfg ChunkConfig, targetScope string, negativeRules []string) ([]SemanticBundle, error) {
	return chunker.BuildSemanticBundles(codesPath, cfg, targetScope, negativeRules)
}

// ProjectAndGroupFiles 跨目录同名投影归并，委托给 chunker 子包
func ProjectAndGroupFiles(files []string, cfg ChunkConfig) []SemanticBundle {
	return chunker.ProjectAndGroupFiles(files, cfg)
}

// ExtractGlobalMacros 扫描提取构建宏，委托给 chunker 子包
func ExtractGlobalMacros(codesPath string) map[string]string {
	return chunker.ExtractGlobalMacros(codesPath)
}

// ExtractHeaderOutline 提取公共头文件大纲，委托给 chunker 子包
func ExtractHeaderOutline(codesPath string, files []string) string {
	return chunker.ExtractHeaderOutline(codesPath, files)
}
