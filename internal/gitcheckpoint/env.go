package gitcheckpoint

import (
	"runtime"
	"strings"

	"github.com/10kkyvl/studioforge/internal/processes"
)

var deniedGitEnvExact = []string{
	"GIT_EXTERNAL_DIFF", "GIT_SSH", "GIT_SSH_COMMAND", "GIT_PROXY_COMMAND",
	"GIT_EDITOR", "EDITOR", "VISUAL", "GIT_PAGER", "PAGER",
	"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_OBJECT_DIRECTORY",
	"GIT_CONFIG", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_COUNT",
	"LD_PRELOAD", "LD_LIBRARY_PATH", "DYLD_INSERT_LIBRARIES", "DYLD_LIBRARY_PATH",
}

var deniedGitEnvPrefixes = []string{
	"GIT_CONFIG_KEY_", "GIT_CONFIG_VALUE_", "GIT_TRACE",
}

func envKeyEqual(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func envKeyHasPrefix(key, prefix string) bool {
	if runtime.GOOS == "windows" {
		return len(key) >= len(prefix) && strings.EqualFold(key[:len(prefix)], prefix)
	}
	return strings.HasPrefix(key, prefix)
}

func DenyGitEnvKey(key string) bool {
	for _, want := range deniedGitEnvExact {
		if envKeyEqual(key, want) {
			return true
		}
	}
	for _, prefix := range deniedGitEnvPrefixes {
		if envKeyHasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func ScrubbedEnvironment() []string {
	return processes.ScrubbedEnvironment(DenyGitEnvKey, []string{
		"GIT_TERMINAL_PROMPT=0",
		"GIT_PAGER=cat",
		"GIT_OPTIONAL_LOCKS=0",
	})
}
