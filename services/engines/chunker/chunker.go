package chunker

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"code-shield/services/coverage"
	"code-shield/services/engines"
)

// ScanAndChunk 扫描 git 仓库中的文件并按目录深度及语义同名投影分组
func ScanAndChunk(codesPath string, cfg engines.ChunkConfig, targetScope string) (map[string][]string, error) {
	bundles, _, err := BuildSemanticBundlesWithPlan(codesPath, cfg, targetScope, nil)
	if err != nil {
		return nil, err
	}

	chunks := make(map[string][]string, len(bundles))
	for _, b := range bundles {
		chunks[b.Name] = b.AllFiles
	}
	return chunks, nil
}

// PlanFiles builds a complete planned-file fact set. It intentionally lists the
// whole repository even in incremental mode so skipped files remain observable.
func PlanFiles(codesPath string, cfg engines.ChunkConfig, targetScope string) (coverage.ScanPlan, error) {
	files, err := listRepositoryFiles(codesPath)
	if err != nil {
		return coverage.ScanPlan{}, err
	}
	incremental := cfg.SinceDays > 0 || cfg.DiffBase != ""
	changedFiles := make(map[string]bool)
	if incremental {
		changedFiles, err = changedPaths(codesPath, cfg)
		if err != nil {
			return coverage.ScanPlan{}, err
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
	plan := coverage.ScanPlan{
		Selected:         make([]coverage.PlannedFile, 0),
		Excluded:         make([]coverage.PlannedFile, 0),
		UnchangedSkipped: make([]coverage.PlannedFile, 0),
		Unknown:          make([]coverage.PlannedFile, 0),
	}

	for _, file := range files {
		if file == "" {
			continue
		}

		// 过滤非源码文件（任务级白名单优先于全局白名单）
		if !IsSourceFile(file, taskExtensions) {
			plan.Excluded = append(plan.Excluded, excludedFile(file, coverage.ReasonNotSourceFile))
			continue
		}

		// 根据 TargetScope 过滤文件
		isTest := IsTestFile(file)
		if targetScope == "business" && isTest {
			plan.Excluded = append(plan.Excluded, excludedFile(file, coverage.ReasonTargetScope))
			continue
		}
		if targetScope == "test" && !isTest {
			plan.Excluded = append(plan.Excluded, excludedFile(file, coverage.ReasonTargetScope))
			continue
		}

		// 过滤自动生成的文件（如 Qt pyuic、protobuf 等）
		if IsGeneratedFile(codesPath, file) {
			plan.Excluded = append(plan.Excluded, excludedFile(file, coverage.ReasonGeneratedFile))
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
				plan.Excluded = append(plan.Excluded, excludedFile(file, coverage.ReasonExcludePath))
				continue
			}
		}

		// 关键字内容过滤
		if len(cfg.ContentKeywords) > 0 {
			matched, err := FileContainsKeywords(filepath.Join(codesPath, file), cfg.ContentKeywords)
			if err != nil {
				log.Printf("[Engine] Failed to check file content for %s: %v\n", file, err)
				plan.Unknown = append(plan.Unknown, excludedFile(file, coverage.ReasonContentFilterError))
				continue
			}
			if !matched {
				plan.Excluded = append(plan.Excluded, excludedFile(file, coverage.ReasonContentKeyword))
				continue
			}
		}

		planned := coverage.PlannedFile{Path: file, DiffTouched: changedFiles[file]}
		if incremental && !changedFiles[file] {
			plan.UnchangedSkipped = append(plan.UnchangedSkipped, planned)
			continue
		}
		plan.Selected = append(plan.Selected, planned)
	}

	plan.Selected = sortPlannedFiles(plan.Selected)
	plan.Excluded = sortPlannedFiles(plan.Excluded)
	plan.UnchangedSkipped = sortPlannedFiles(plan.UnchangedSkipped)
	plan.Unknown = sortPlannedFiles(plan.Unknown)
	if cfg.DiffBase != "" {
		for i := range plan.Selected {
			plan.Selected[i].HunkRanges = diffHunkRanges(codesPath, cfg.DiffBase, plan.Selected[i].Path)
		}
	}

	return plan, nil
}

func GetFilteredFiles(codesPath string, cfg engines.ChunkConfig, targetScope string) ([]string, error) {
	plan, err := PlanFiles(codesPath, cfg, targetScope)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(plan.Selected))
	for _, planned := range plan.Selected {
		files = append(files, planned.Path)
	}
	return files, nil
}

func listRepositoryFiles(codesPath string) ([]string, error) {
	cmd := exec.Command("git", "-C", codesPath, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	output, err := cmd.Output()
	if err == nil {
		rawFiles := bytes.Split(output, []byte{0})
		files := make([]string, 0, len(rawFiles))
		for _, file := range rawFiles {
			path := filepath.ToSlash(filepath.Clean(string(file)))
			if path != "" && path != "." {
				files = append(files, path)
			}
		}
		return files, nil
	}

	var files []string
	_ = filepath.Walk(codesPath, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return nil
		}
		if rel, relErr := filepath.Rel(codesPath, path); relErr == nil {
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	return files, nil
}

func changedPaths(codesPath string, cfg engines.ChunkConfig) (map[string]bool, error) {
	var cmd *exec.Cmd
	if cfg.SinceDays > 0 {
		cmd = exec.Command("git", "-C", codesPath, "log", fmt.Sprintf("--since=%d days ago", cfg.SinceDays), "--name-only", "--pretty=format:")
	} else {
		cmd = exec.Command("git", "-C", codesPath, "diff", "--name-only", cfg.DiffBase)
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("resolve incremental files: %w", err)
	}
	changed := make(map[string]bool)
	for _, raw := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		path := filepath.ToSlash(strings.TrimSpace(raw))
		if path != "" {
			if _, err := os.Stat(filepath.Join(codesPath, path)); err == nil {
				changed[path] = true
			}
		}
	}
	log.Printf("[IncrementalChunk] Found %d changed files", len(changed))
	return changed, nil
}

func diffHunkRanges(codesPath, diffBase, path string) []string {
	cmd := exec.Command("git", "-C", codesPath, "diff", "--unified=0", "--no-ext-diff", diffBase, "--", path)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	ranges := make([]string, 0)
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(line, "@@") {
			continue
		}
		parts := strings.Split(line, "+")
		if len(parts) < 2 {
			continue
		}
		spec := strings.Split(strings.TrimSpace(parts[1]), ",")
		start := strings.TrimPrefix(spec[0], "+")
		length := "1"
		if len(spec) > 1 {
			length = spec[1]
		}
		if length == "0" {
			continue
		}
		end := start
		if startNumber, err := strconv.Atoi(start); err == nil {
			if number, err := strconv.Atoi(length); err == nil && number > 1 {
				end = strconv.Itoa(startNumber + number - 1)
			}
		}
		ranges = append(ranges, start+"-"+end)
	}
	return ranges
}

func excludedFile(path, reason string) coverage.PlannedFile {
	return coverage.PlannedFile{Path: path, Reason: reason}
}

func sortPlannedFiles(items []coverage.PlannedFile) []coverage.PlannedFile {
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	return items
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
