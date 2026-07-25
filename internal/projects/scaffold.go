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

// EnsureReference places a reference document StudioForge ships into a project,
// so an agent can open it on demand whatever provider is running: Claude reads
// it with its own tools, OpenRouter and NVIDIA through agenttools, which is
// sandboxed to the project directory and could not reach it anywhere else.
//
// It is write-once. An operator who edits the file — or deletes it because their
// game does not want those rules — keeps their version, because the whole point
// of shipping it into the project rather than into the prompt is that it is
// theirs to change. It is separate from Scaffold for the same reason it is
// separate from LoadContext's two files: Scaffold bails out on a directory that
// already has a manifest, and a project created before this document existed
// still needs it.
func EnsureReference(root, relative, body string) error {
	path := filepath.Join(root, filepath.FromSlash(relative))
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect reference document: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create reference directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return fmt.Errorf("write reference document: %w", err)
	}
	return nil
}
