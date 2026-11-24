package defectlifecycle

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// repoFileIndex 保存某次扫描所对应的仓库文件存在性快照（全小写归一化路径）。
// Git 仓库优先基于不可变的 Commit Tree 构建，避免受工作区并发切换或大小写敏感影响；
// 非 Git 目录仅在根目录有效时通过遍历构建归一化小写索引，任何异常一律 fail-closed 返回 nil。
type repoFileIndex struct {
	source     string
	commit     string
	files      map[string]bool
	submodules []string
}

func buildRepoFileIndex(repoRoot, headCommit string) *repoFileIndex {
	root := strings.TrimSpace(repoRoot)
	if root == "" {
		return nil
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil
	}

	_, gitErr := os.Stat(filepath.Join(root, ".git"))
	if gitErr == nil {
		return buildGitTreeFileIndex(root, headCommit)
	}
	if os.IsNotExist(gitErr) {
		return buildFSWalkFileIndex(root)
	}
	return nil
}

func buildGitTreeFileIndex(repoRoot, headCommit string) *repoFileIndex {
	ref := strings.TrimSpace(headCommit)
	if ref == "" {
		ref = "HEAD"
	}

	out, err := exec.Command("git", "-C", repoRoot, "ls-tree", "-r", "-z", ref).Output()
	if err != nil {
		return nil
	}

	files := make(map[string]bool)
	var submodules []string
	for _, entry := range strings.Split(string(out), "\x00") {
		if entry == "" {
			continue
		}
		tabIdx := strings.IndexByte(entry, '\t')
		if tabIdx <= 0 || tabIdx+1 >= len(entry) {
			continue
		}
		meta := entry[:tabIdx]
		relPath := entry[tabIdx+1:]
		norm := NormalizePath("", relPath)
		if norm == "" {
			continue
		}
		fields := strings.Fields(meta)
		if len(fields) >= 2 && (fields[0] == "160000" || fields[1] == "commit") {
			submodules = append(submodules, strings.TrimSuffix(norm, "/")+"/")
		}
		files[norm] = true
	}

	if len(files) == 0 {
		return nil
	}

	return &repoFileIndex{
		source:     "git_tree",
		commit:     ref,
		files:      files,
		submodules: submodules,
	}
}

func buildFSWalkFileIndex(repoRoot string) *repoFileIndex {
	files := make(map[string]bool)
	walkErr := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			return relErr
		}
		norm := NormalizePath("", rel)
		if norm != "" {
			files[norm] = true
		}
		return nil
	})
	if walkErr != nil || len(files) == 0 {
		return nil
	}
	return &repoFileIndex{
		source: "fs_walk",
		files:  files,
	}
}

// IsDefinitelyMissing 仅当索引完整有效且目标路径既不在文件集合中、也不属于子模块子树时返回 true。
func (idx *repoFileIndex) IsDefinitelyMissing(normPath string) bool {
	if idx == nil || len(idx.files) == 0 {
		return false
	}
	norm := NormalizePath("", normPath)
	if norm == "" {
		return false
	}
	if idx.files[norm] {
		return false
	}
	for _, sub := range idx.submodules {
		if strings.HasPrefix(norm, sub) {
			return false
		}
	}
	return true
}
