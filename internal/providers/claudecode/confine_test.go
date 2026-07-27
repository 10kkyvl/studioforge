package claudecode

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeSelf(path string) func() (string, error) {
	return func() (string, error) { return path, nil }
}

// bypassPermissions ignores deny rules and skips PreToolUse hooks entirely, so
// writing a settings file for danger-full-access would claim a containment that
// does not exist. Saying so is the honest option; pretending is not.
func TestConfinementIsNotClaimedForDangerFullAccess(t *testing.T) {
	dir := t.TempDir()
	if _, _, ok := writeConfinement(dir, "danger-full-access", "", "run-1", fakeSelf("sf.exe")); ok {
		t.Error("danger-full-access maps to bypassPermissions, which honours neither deny rules nor hooks")
	}
}

func TestConfinementAppliesToTheProfilesThatHonourIt(t *testing.T) {
	dir := t.TempDir()
	for _, profile := range []string{"read-only", "workspace-write"} {
		path, release, ok := writeConfinement(dir, profile, "", "run-1", fakeSelf("sf.exe"))
		if !ok {
			t.Fatalf("%s should be confined", profile)
		}
		t.Cleanup(release)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var settings confinementSettings
		if err := json.Unmarshal(body, &settings); err != nil {
			t.Fatal(err)
		}
		if len(settings.Hooks.PreToolUse) != 1 {
			t.Fatalf("%s: hooks=%+v", profile, settings.Hooks.PreToolUse)
		}
		hook := settings.Hooks.PreToolUse[0]
		if !strings.Contains(hook.Matcher, "Read") || !strings.Contains(hook.Matcher, "Write") {
			t.Errorf("%s: matcher=%q must cover the file tools", profile, hook.Matcher)
		}
		if strings.Contains(hook.Matcher, "Bash") {
			t.Errorf("%s: a shell command cannot be reduced to a path; claiming to guard Bash would be a lie", profile)
		}
		if len(hook.Hooks) != 1 || !strings.Contains(hook.Hooks[0].Command, "claude-guard") {
			t.Errorf("%s: hook command=%+v", profile, hook.Hooks)
		}
		if !strings.Contains(hook.Hooks[0].Command, dir) {
			t.Errorf("%s: the guard has to be told which project to confine to: %q", profile, hook.Hooks[0].Command)
		}
		if len(settings.Permissions.Deny) == 0 {
			t.Errorf("%s: the deny backstop is missing", profile)
		}
	}
}

// A project under the operator's home directory must not be denied along with
// the secrets, which is what a sweeping ~/** rule would do.
func TestConfinementDenyRulesNameCredentialStoresNotWholeHome(t *testing.T) {
	for _, rule := range denyRules() {
		if strings.Contains(rule, "(~/**") || strings.Contains(rule, "(/**") {
			t.Errorf("rule %q would deny a project living inside the operator's home", rule)
		}
	}
}

func TestConfinementCleansUpAfterItself(t *testing.T) {
	dir := t.TempDir()
	path, release, ok := writeConfinement(dir, "workspace-write", "", "run-1", fakeSelf("sf.exe"))
	if !ok {
		t.Fatal("expected confinement")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	release()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the generated settings file outlived the run: %v", err)
	}
}

func TestConfinementDegradesWhenTheExecutableCannotBeLocated(t *testing.T) {
	dir := t.TempDir()
	failing := func() (string, error) { return "", os.ErrNotExist }
	if _, _, ok := writeConfinement(dir, "workspace-write", "", "run-1", failing); ok {
		t.Error("without our own path there is no hook to register; the run must degrade, not carry a broken hook")
	}
}

func TestWithSettingsKeepsThePromptLast(t *testing.T) {
	args := withSettings([]string{"-p", "--verbose", "--", "do the thing"}, "cfg.json")
	if args[len(args)-1] != "do the thing" || args[len(args)-2] != "--" {
		t.Fatalf("prompt must stay behind the separator: %q", args)
	}
	found := false
	for i, arg := range args {
		if arg == "--settings" {
			found = args[i+1] == "cfg.json"
		}
	}
	if !found {
		t.Errorf("--settings missing or unpaired: %q", args)
	}
}

// The guard sits in front of every file tool call, so its failure modes matter
// as much as its successes: anything it cannot judge is deferred to Claude
// Code's ordinary permission flow rather than refused.
func TestGuardDeniesAPathOutsideTheProject(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	var out bytes.Buffer
	Guard(strings.NewReader(`{"hook_event_name":"PreToolUse","tool_name":"Read","tool_input":{"file_path":`+quote(outside)+`}}`), &out, root)
	if !strings.Contains(out.String(), `"permissionDecision":"deny"`) {
		t.Fatalf("a read outside the project must be refused, got %q", out.String())
	}
	if !strings.Contains(out.String(), `"hookEventName":"PreToolUse"`) {
		t.Errorf("the decision must name the event it answers: %q", out.String())
	}
}

func TestGuardAllowsAPathInsideTheProject(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "src", "main.lua")
	var out bytes.Buffer
	Guard(strings.NewReader(`{"hook_event_name":"PreToolUse","tool_name":"Write","tool_input":{"file_path":`+quote(inside)+`}}`), &out, root)
	if out.Len() != 0 {
		t.Errorf("a path inside the project must be deferred, not decided: %q", out.String())
	}
}

func TestGuardReadsEveryPathArgumentShape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "elsewhere")
	for _, key := range []string{"file_path", "notebook_path", "path"} {
		var out bytes.Buffer
		Guard(strings.NewReader(`{"tool_name":"Read","tool_input":{"`+key+`":`+quote(outside)+`}}`), &out, root)
		if !strings.Contains(out.String(), `"deny"`) {
			t.Errorf("%s outside the project was not refused: %q", key, out.String())
		}
	}
}

func TestGuardDefersWhatItCannotJudge(t *testing.T) {
	root := t.TempDir()
	for name, input := range map[string]string{
		"malformed json":  `{not json`,
		"empty":           ``,
		"no tool input":   `{"tool_name":"Read"}`,
		"no path":         `{"tool_name":"Bash","tool_input":{"command":"ls"}}`,
		"blank path":      `{"tool_name":"Read","tool_input":{"file_path":""}}`,
		"unknown key":     `{"tool_name":"Future","tool_input":{"target_file":"/etc/passwd"}}`,
		"path not string": `{"tool_name":"Read","tool_input":{"file_path":42}}`,
	} {
		var out bytes.Buffer
		Guard(strings.NewReader(input), &out, root)
		if out.Len() != 0 {
			t.Errorf("%s: guard must defer rather than refuse what it cannot understand, got %q", name, out.String())
		}
	}
}

func TestGuardDefersWhenTheProjectRootIsUnusable(t *testing.T) {
	var out bytes.Buffer
	Guard(strings.NewReader(`{"tool_name":"Read","tool_input":{"file_path":"/etc/passwd"}}`), &out, filepath.Join(t.TempDir(), "does-not-exist"))
	if out.Len() != 0 {
		t.Errorf("without a resolvable root there is nothing to measure against: %q", out.String())
	}
}

func quote(s string) string {
	body, _ := json.Marshal(s)
	return string(body)
}
