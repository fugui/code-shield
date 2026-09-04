package chunker

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"code-shield/services/engines"
)

// ScanAndChunk 扫描 git 仓库中的文件并按目录深度及语义同名投影分组
func ScanAndChunk(codesPath string, cfg engines.ChunkConfig, targetScope string) (map[string][]string, error) {
	bundles, err := BuildSemanticBundles(codesPath, cfg, targetScope, nil)
	if err != nil {
		return nil, err
	}

	chunks := make(map[string][]string, len(bundles))
	for _, b := range bundles {
		chunks[b.Name] = b.AllFiles
	}
	return chunks, nil
}

// GetFilteredFiles 提取并过滤满足配置条件的源文件列表
func GetFilteredFiles(codesPath string, cfg engines.ChunkConfig, targetScope string) ([]string, error) {
	var files []string

	// 1. 若配置了 SinceDays，仅提取最近 N 天提交发生变动的文件
	if cfg.SinceDays > 0 {
		sinceArg := fmt.Sprintf("--since=%d days ago", cfg.SinceDays)
		cmd := exec.Command("git", "-C", codesPath, "log", sinceArg, "--name-only", "--pretty=format:")
		if out, err := cmd.Output(); err == nil {
			rawLines := strings.Split(strings.TrimSpace(string(out)), "\n")
			seen := make(map[string]bool)
			for _, line := range rawLines {
				trimmed := strings.TrimSpace(line)
				if trimmed != "" && !seen[trimmed] {
					seen[trimmed] = true
					if _, statErr := os.Stat(filepath.Join(codesPath, trimmed)); statErr == nil {
						files = append(files, filepath.ToSlash(trimmed))
					}
				}
			}
			log.Printf("[IncrementalChunk] Found %d changed files in the last %d days", len(files), cfg.SinceDays)
		}
	} else if cfg.DiffBase != "" {
		// 2. 若配置了 DiffBase，提取与基线分支/commit 发生 diff 的文件
		cmd := exec.Command("git", "-C", codesPath, "diff", "--name-only", cfg.DiffBase)
		if out, err := cmd.Output(); err == nil {
			rawLines := strings.Split(strings.TrimSpace(string(out)), "\n")
			for _, line := range rawLines {
				trimmed := strings.TrimSpace(line)
				if trimmed != "" {
					if _, statErr := os.Stat(filepath.Join(codesPath, trimmed)); statErr == nil {
						files = append(files, filepath.ToSlash(trimmed))
					}
				}
			}
			log.Printf("[IncrementalChunk] Found %d diff files against base %s", len(files), cfg.DiffBase)
		}
	}

	// 3. 默认全量模式：提取全仓 git ls-files
	if len(files) == 0 && cfg.SinceDays == 0 && cfg.DiffBase == "" {
		cmd := exec.Command("git", "-C", codesPath, "ls-files")
		output, err := cmd.Output()
		if err != nil {
			// 降级为物理文件遍历 (兼容非 git 仓库或单测 Mock 环境)
			_ = filepath.Walk(codesPath, func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return nil
				}
				rel, relErr := filepath.Rel(codesPath, path)
				if relErr == nil {
					files = append(files, filepath.ToSlash(rel))
				}
				return nil
			})
		} else {
			files = strings.Split(strings.TrimSpace(string(output)), "\n")
		}
	}

	// 构建任务级扩展名白名单（为空时 isSourceFile 回退到全局白名单）
	var taskExtensions map[string]bool
	if len(cfg.FileExtensions) > 0 {
		taskExtensions = make(map[string]bool, len(cfg.FileExtensions))
		for _, ext := range cfg.FileExtensions {
			if !strings.HasPrefix(ext, ".") {
				ext = "." + ext
			}
			taskExtensions[strings.ToLower(ext)] = true
		}
	}
	var filtered []string

	for _, file := range files {
		if file == "" {
			continue
		}

		// 过滤非源码文件（任务级白名单优先于全局白名单）
		if !IsSourceFile(file, taskExtensions) {
			continue
		}

		// 根据 TargetScope 过滤文件
		isTest := IsTestFile(file)
		if targetScope == "business" && isTest {
			continue
		}
		if targetScope == "test" && !isTest {
			continue
		}

		// 过滤自动生成的文件（如 Qt pyuic、protobuf 等）
		if IsGeneratedFile(codesPath, file) {
			continue
		}

		// 自定义忽略路径过滤
		if len(cfg.ExcludePaths) > 0 {
			excluded := false
			normalizedFile := filepath.ToSlash(file)
			for _, skipPath := range cfg.ExcludePaths {
				normalizedSkip := filepath.ToSlash(skipPath)
				if strings.HasPrefix(normalizedFile, normalizedSkip) || strings.Contains(normalizedFile, "/"+normalizedSkip) {
					excluded = true
					break
				}
			}
			if excluded {
				continue
			}
		}

		// 关键字内容过滤
		if len(cfg.ContentKeywords) > 0 {
			matched, err := FileContainsKeywords(filepath.Join(codesPath, file), cfg.ContentKeywords)
			if err != nil {
				log.Printf("[Engine] Failed to check file content for %s: %v\n", file, err)
				continue
			}
			if !matched {
				continue
			}
		}

		filtered = append(filtered, file)
	}

	// 确定性稳定排序：按路径字典序排序，保证多次扫描切块边界绝对稳定
	sort.Strings(filtered)

	return filtered, nil
}

// FileContainsKeywords 检测文件内容是否包含任意给定的关键字（高效流式读取）
func FileContainsKeywords(filePath string, keywords []string) (bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return false, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		for _, kw := range keywords {
			if strings.Contains(line, kw) {
				return true, nil
			}
		}
	}
	return false, scanner.Err()
}

// sourceExtensions 定义需要分析的源码文件扩展名
var sourceExtensions = map[string]bool{
	// 通用编程语言
	".go": true, ".py": true, ".java": true, ".kt": true, ".scala": true,
	".js": true, ".ts": true, ".jsx": true, ".tsx": true, ".vue": true, ".svelte": true,
	".c": true, ".cpp": true, ".cc": true, ".cxx": true, ".h": true, ".hpp": true,
	".cs": true, ".rs": true, ".rb": true, ".php": true, ".swift": true, ".m": true,
	".dart": true, ".lua": true, ".r": true, ".pl": true, ".pm": true,
	// Shell / 脚本
	".sh": true, ".bash": true, ".zsh": true, ".bat": true, ".ps1": true,
	// 数据库
	".sql": true,
	// 其他
	".proto": true, ".graphql": true, ".gql": true,
	".tf": true, ".hcl": true,
	".dockerfile": true,
}

// IsSourceFile 根据扩展名判断是否为源码文件。
func IsSourceFile(file string, taskExtensions map[string]bool) bool {
	// 跳过 . 开头的目录（如 .github/, .vscode/, .idea/ 等）
	for _, part := range strings.Split(file, "/") {
		if strings.HasPrefix(part, ".") && part != "." {
			return false
		}
	}

	// 跳过常见的非源码目录
	lower := strings.ToLower(file)
	for _, skip := range []string{"vendor/", "node_modules/", "__pycache__/", "dist/", "build/", "thirdparts/", "thirdparty/", "third_party/", "3rdparty/"} {
		if strings.Contains(lower, skip) {
			return false
		}
	}

	ext := strings.ToLower(filepath.Ext(file))
	if ext == "" {
		if taskExtensions != nil {
			return false
		}
		base := strings.ToLower(filepath.Base(file))
		return base == "dockerfile" || base == "makefile" || base == "rakefile" || base == "gemfile"
	}

	if taskExtensions != nil {
		return taskExtensions[ext]
	}
	return sourceExtensions[ext]
}

// IsTestFile 根据文件名和路径判断是否为测试文件
func IsTestFile(file string) bool {
	base := filepath.Base(file)
	lower := strings.ToLower(base)

	if strings.HasSuffix(lower, "_test.go") {
		return true
	}
	if strings.Contains(lower, ".test.") || strings.Contains(lower, ".spec.") {
		return true
	}
	if strings.HasSuffix(lower, ".py") && (strings.HasPrefix(lower, "test_") || strings.HasSuffix(strings.TrimSuffix(lower, ".py"), "_test")) {
		return true
	}
	if strings.HasSuffix(base, "Test.java") || strings.HasSuffix(base, "Spec.java") || strings.HasSuffix(base, "Test.kt") {
		return true
	}

	for _, ext := range []string{".cpp", ".cc", ".c", ".cxx", ".h", ".hpp", ".hxx"} {
		if strings.HasSuffix(lower, ext) {
			nameNoExt := strings.TrimSuffix(lower, ext)
			if strings.HasPrefix(nameNoExt, "test_") || strings.HasSuffix(nameNoExt, "_test") || strings.HasSuffix(nameNoExt, "_unittest") {
				return true
			}
		}
	}

	lowerPath := strings.ToLower(file)
	for _, dir := range []string{"test/", "tests/", "__tests__/", "spec/", "testdata/"} {
		if strings.Contains(lowerPath, dir) {
			return true
		}
	}

	return false
}

// generatedMarkers 常见自动生成文件的标记
var generatedMarkers = []string{
	"# Form implementation generated from reading ui file", // Qt pyuic5/pyuic6
	"# Created by: PyQt", // PyQt UI code generator
	"# WARNING! All changes made in this file will be lost", // Qt Designer
	"// Code generated by",                        // Go generate / protobuf
	"// DO NOT EDIT",                              // 通用自动生成标记
	"# This file is automatically generated",      // 通用 Python/Shell
	"/* This file is auto-generated",              // 通用 C/C++/Java
	"// This code was generated by",               // gRPC / Swagger
	"# Generated by the protocol buffer compiler", // protobuf Python
}

// IsGeneratedFile 读取文件头部前 10 行，检查是否包含自动生成标记
func IsGeneratedFile(codesPath, file string) bool {
	f, err := os.Open(filepath.Join(codesPath, file))
	if err != nil {
		return false
	}
	defer f.Close()

	reader := bufio.NewReader(f)
	for i := 0; i < 10; i++ {
		line, err := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		for _, marker := range generatedMarkers {
			if strings.Contains(line, marker) {
				log.Printf("[ChunkedEngine] Skipping generated file: %s\n", file)
				return true
			}
		}
		if err == io.EOF {
			break
		}
	}
	return false
}
