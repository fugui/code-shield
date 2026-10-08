package defectlifecycle

import (
	"os/exec"
	"strings"
)

func BuildRenameTargets(repoRoot string) map[string]string {
	targets := make(map[string]string)
	if repoRoot == "" {
		return targets
	}
	for _, args := range [][]string{
		{"diff", "--cached", "--name-status", "-M", "--diff-filter=R", "HEAD"},
		{"diff", "--name-status", "-M", "--diff-filter=R", "HEAD"},
	} {
		output, err := exec.Command("git", append([]string{"-C", repoRoot}, args...)...).Output()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(output), "\n") {
			parts := strings.Split(line, "\t")
			if len(parts) == 3 {
				targets[normalizeGitPath(parts[2])] = normalizeGitPath(parts[1])
			}
		}
	}
	return targets
}

func normalizeGitPath(path string) string {
	return strings.Trim(strings.TrimSpace(path), `"`)
}
