package services

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// SemanticBundle 语义感知分片数据包
type SemanticBundle struct {
	Name          string            `json:"name"`           // 分片名称
	PrimaryFiles  []string          `json:"primary_files"`  // 核心实现文件 (.cc/.cpp/.go/.java)
	HeaderFiles   []string          `json:"header_files"`   // 跨目录配对头文件 (.h/.hpp)
	AllFiles      []string          `json:"all_files"`      // 分片包含的全部有效文件
	MacroContext  map[string]string `json:"macro_context"`  // 提取的构建宏定义 {"FMT_USE_GRISU": "0"}
	NegativeRules []string          `json:"negative_rules"` // 历史负样本与例外规则
	HeaderOutline string            `json:"header_outline"` // 基础公用头文件声明摘要 (Header Outline)
}

// BuildSemanticBundles 扫描仓库文件并构建语义感知分片
func BuildSemanticBundles(codesPath string, cfg ChunkConfig, targetScope string, negativeRules []string) ([]SemanticBundle, error) {
	// 1. 获取过滤后的源文件列表
	filteredFiles, err := getFilteredFiles(codesPath, cfg, targetScope)
	if err != nil {
		return nil, err
	}
	if len(filteredFiles) == 0 {
		return []SemanticBundle{}, nil
	}

	// 2. 提取全局构建宏与配置
	macroContext := extractGlobalMacros(codesPath)

	// 3. 提取核心基础头文件轻量声明摘要 (Header Outline)
	headerOutline := extractHeaderOutline(codesPath, filteredFiles)

	// 4. 执行跨目录同名投影映射 (Cross-Directory Projection)
	bundles := projectAndGroupFiles(filteredFiles, cfg)

	// 5. 注入宏、摘要与负样本规则
	for i := range bundles {
		bundles[i].MacroContext = macroContext
		bundles[i].HeaderOutline = headerOutline
		bundles[i].NegativeRules = negativeRules
	}

	return bundles, nil
}

// projectAndGroupFiles 将源文件与配对头文件归并为语义分片
func projectAndGroupFiles(files []string, cfg ChunkConfig) []SemanticBundle {
	// 构建文件名基名查找索引 (BaseName -> Header List)
	headerMap := make(map[string][]string)
	var implFiles []string

	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f))
		baseWithoutExt := strings.TrimSuffix(filepath.Base(f), ext)

		if ext == ".h" || ext == ".hpp" || ext == ".hxx" {
			headerMap[baseWithoutExt] = append(headerMap[baseWithoutExt], f)
		} else {
			implFiles = append(implFiles, f)
		}
	}

	pairedHeaders := make(map[string]bool)
	type fileGroup struct {
		primary []string
		headers []string
	}
	rawGroups := make(map[string]*fileGroup)

	// 先处理实现文件的目录归组与头文件绑定
	for _, f := range implFiles {
		ext := strings.ToLower(filepath.Ext(f))
		baseWithoutExt := strings.TrimSuffix(filepath.Base(f), ext)

		chunkName := getDirectoryChunkName(f, cfg.Depth)
		if rawGroups[chunkName] == nil {
			rawGroups[chunkName] = &fileGroup{}
		}
		rawGroups[chunkName].primary = append(rawGroups[chunkName].primary, f)

		// 跨目录投影绑定：若存在同名头文件（如 include/fmt/posix.h 对应 src/posix.cc），自动归入同一 chunk
		if hList, ok := headerMap[baseWithoutExt]; ok {
			for _, h := range hList {
				if !pairedHeaders[h] {
					rawGroups[chunkName].headers = append(rawGroups[chunkName].headers, h)
					pairedHeaders[h] = true
				}
			}
		}
	}

	// 收集未配对的独立头文件
	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f))
		if (ext == ".h" || ext == ".hpp" || ext == ".hxx") && !pairedHeaders[f] {
			chunkName := getDirectoryChunkName(f, cfg.Depth)
			if rawGroups[chunkName] == nil {
				rawGroups[chunkName] = &fileGroup{}
			}
			rawGroups[chunkName].headers = append(rawGroups[chunkName].headers, f)
		}
	}

	// 按照 MaxFiles 分片拆分与排序
	var bundles []SemanticBundle
	keys := make([]string, 0, len(rawGroups))
	for k := range rawGroups {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	maxFiles := cfg.MaxFiles
	if maxFiles <= 0 {
		maxFiles = 8 // 调优收敛：单分片默认 8 个文件，防止大模型上下文注意力衰减与长尾文件漏扫
	}

	for _, name := range keys {
		grp := rawGroups[name]
		allGroupFiles := append([]string{}, grp.primary...)
		allGroupFiles = append(allGroupFiles, grp.headers...)

		if len(allGroupFiles) > maxFiles {
			// 超额拆分
			for i := 0; i < len(allGroupFiles); i += maxFiles {
				end := i + maxFiles
				if end > len(allGroupFiles) {
					end = len(allGroupFiles)
				}
				subSlice := allGroupFiles[i:end]
				subName := fmt.Sprintf("%s-%d", name, i/maxFiles+1)

				var subPrimary, subHeaders []string
				for _, sf := range subSlice {
					ext := strings.ToLower(filepath.Ext(sf))
					if ext == ".h" || ext == ".hpp" || ext == ".hxx" {
						subHeaders = append(subHeaders, sf)
					} else {
						subPrimary = append(subPrimary, sf)
					}
				}

				bundles = append(bundles, SemanticBundle{
					Name:         subName,
					PrimaryFiles: subPrimary,
					HeaderFiles:  subHeaders,
					AllFiles:     subSlice,
				})
			}
		} else {
			bundles = append(bundles, SemanticBundle{
				Name:         name,
				PrimaryFiles: grp.primary,
				HeaderFiles:  grp.headers,
				AllFiles:     allGroupFiles,
			})
		}
	}

	return bundles
}

// getDirectoryChunkName 计算文件的分片目录名
func getDirectoryChunkName(file string, depth int) string {
	parts := strings.Split(file, string(filepath.Separator))
	if len(parts) <= 1 {
		return "root"
	}
	if depth <= 0 {
		depth = 1
	}
	if depth >= len(parts) {
		depth = len(parts) - 1
	}
	return filepath.Join(parts[:depth]...)
}

// extractGlobalMacros 扫描构建文件（如 CMakeLists.txt 等）提取宏开关
func extractGlobalMacros(codesPath string) map[string]string {
	macros := make(map[string]string)
	candidates := []string{
		"CMakeLists.txt",
		"Makefile",
		"config.h",
		"config.h.in",
	}

	reDefine := regexp.MustCompile(`(?i)#\s*define\s+([A-Za-z0-9_]+)\s+([^\s/]+)`)
	reCmakeSet := regexp.MustCompile(`(?i)set\s*\(\s*([A-Za-z0-9_]+)\s+([^\s\)]+)\s*\)`)
	reCmakeDef := regexp.MustCompile(`(?i)add_definitions\s*\(\s*-D([A-Za-z0-9_]+)=?([^\s\)]*)\s*\)`)

	for _, cand := range candidates {
		fullPath := filepath.Join(codesPath, cand)
		f, err := os.Open(fullPath)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			if m := reDefine.FindStringSubmatch(line); len(m) >= 3 {
				macros[m[1]] = m[2]
			}
			if m := reCmakeSet.FindStringSubmatch(line); len(m) >= 3 {
				macros[m[1]] = m[2]
			}
			if m := reCmakeDef.FindStringSubmatch(line); len(m) >= 2 {
				val := "1"
				if len(m) >= 3 && m[2] != "" {
					val = m[2]
				}
				macros[m[1]] = val
			}
		}
		if scanErr := scanner.Err(); scanErr != nil {
			log.Printf("[SemanticChunker] Warning scanning %s for macros: %v", cand, scanErr)
		}
		f.Close()
	}

	return macros
}

// extractHeaderOutline 针对核心公共头文件提取结构体与函数声明摘要（~30-50 行）
func extractHeaderOutline(codesPath string, files []string) string {
	var outlineBuilder strings.Builder
	coreHeaderNames := []string{"core.h", "common.h", "types.h", "defs.h", "format.h", "base.h"}

	reDeclaration := regexp.MustCompile(`(?m)^\s*(class|struct|enum class|enum)\s+([A-Za-z0-9_]+)`)
	reFuncSig := regexp.MustCompile(`(?m)^\s*([A-Za-z0-9_:<>&*]+\s+[A-Za-z0-9_]+\s*\([^;{}]*\)\s*(const|noexcept)?\s*;)`)

	outlineCount := 0
	for _, f := range files {
		base := strings.ToLower(filepath.Base(f))
		isCore := false
		for _, cn := range coreHeaderNames {
			if base == cn {
				isCore = true
				break
			}
		}
		if !isCore {
			continue
		}

		fullPath := filepath.Join(codesPath, f)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}

		content := string(data)
		outlineBuilder.WriteString("// --- Header Outline: ")
		outlineBuilder.WriteString(f)
		outlineBuilder.WriteString(" ---\n")

		// 提取类与结构体声明
		classes := reDeclaration.FindAllString(content, 10)
		for _, c := range classes {
			outlineBuilder.WriteString(strings.TrimSpace(c))
			outlineBuilder.WriteString(" { ... };\n")
			outlineCount++
		}

		// 提取核心方法声明
		funcs := reFuncSig.FindAllString(content, 15)
		for _, fn := range funcs {
			outlineBuilder.WriteString(strings.TrimSpace(fn))
			outlineBuilder.WriteString("\n")
			outlineCount++
		}

		if outlineCount >= 40 {
			break
		}
	}

	return strings.TrimSpace(outlineBuilder.String())
}
