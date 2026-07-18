package projects

import (
	"fmt"
	"os"
	"path/filepath"
)

// Scaffold writes a minimal Rojo project skeleton into root: a
// default.project.json manifest plus src/server and src/client
// directories with a placeholder script each. It is a no-op (beyond
// ensuring the directories exist) when a default.project.json is already
// present, so it never clobbers an existing workspace.
func Scaffold(root, name string) error {
	manifest := filepath.Join(root, "default.project.json")
	if _, err := os.Stat(manifest); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect project manifest: %w", err)
	}
	serverDir := filepath.Join(root, "src", "server")
	clientDir := filepath.Join(root, "src", "client")
	for _, dir := range []string{serverDir, clientDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create scaffold directory: %w", err)
		}
	}
	files := map[string]string{
		manifest: fmt.Sprintf("{\n  \"name\": %q,\n  \"tree\": {\n    \"$className\": \"DataModel\",\n    \"ServerScriptService\": {\"$path\": \"src/server\"},\n    \"StarterPlayer\": {\"StarterPlayerScripts\": {\"$path\": \"src/client\"}}\n  }\n}\n", name),
		filepath.Join(serverDir, "Main.server.lua"): "-- Placeholder server entry point for " + name + ".\n",
		filepath.Join(clientDir, "Main.client.lua"): "-- Placeholder client entry point for " + name + ".\n",
	}
	for path, body := range files {
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			return fmt.Errorf("write scaffold file: %w", err)
		}
	}
	return nil
}
