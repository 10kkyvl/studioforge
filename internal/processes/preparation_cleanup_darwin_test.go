//go:build darwin

package processes

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestRejectedStartReleasesPreparedTempAndProxy(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec unavailable")
	}
	for _, reason := range []string{"shutting down", "already exists"} {
		t.Run(reason, func(t *testing.T) {
			root, tempRoot := t.TempDir(), t.TempDir()
			t.Setenv("TMPDIR", tempRoot)
			supervisor := NewSupervisor()
			if reason == "shutting down" {
				supervisor.closing = true
			} else {
				supervisor.reserving["rejected"] = struct{}{}
			}
			preparedDarwinProxies.Lock()
			before := make(map[*exec.Cmd]bool)
			for cmd := range preparedDarwinProxies.byCommand {
				before[cmd] = true
			}
			preparedDarwinProxies.Unlock()
			_, err := supervisor.Start(context.Background(), Spec{
				ID: "rejected", Executable: os.Args[0], WorkingDirectory: root,
				Containment: ContainmentSpec{
					Mode: ContainmentRequired, WorkspaceRoot: root, Filesystem: FilesystemProjectOnly,
					Network: NetworkRegistryOnly, RegistryHosts: []string{"registry.npmjs.org"},
				},
			})
			if err == nil || !strings.Contains(err.Error(), reason) {
				t.Fatalf("expected pre-start rejection %q, got %v", reason, err)
			}
			preparedDarwinProxies.Lock()
			var leaked []*exec.Cmd
			for cmd := range preparedDarwinProxies.byCommand {
				if !before[cmd] {
					leaked = append(leaked, cmd)
				}
			}
			preparedDarwinProxies.Unlock()
			for _, cmd := range leaked {
				cleanupPreparedPlatformContainment(cmd)
			}
			if len(leaked) != 0 {
				t.Errorf("rejected Start leaked %d proxy listeners", len(leaked))
			}
			entries, err := os.ReadDir(tempRoot)
			if err != nil || len(entries) != 0 {
				t.Errorf("rejected Start leaked temp directories: %v, err=%v", entries, err)
			}
		})
	}
}
