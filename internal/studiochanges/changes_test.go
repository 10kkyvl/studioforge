package studiochanges

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeStructuredAndOpaqueOperations(t *testing.T) {
	var args map[string]any
	if err := json.Unmarshal([]byte(`{"edits":[{"path":"Workspace.Part","operation":"create","properties":{"Size":[1,2,3],"Name":"secret-value"}},{"path":"Workspace.Old","action":"delete"},{"path":"Workspace.Child","action":"reparent"}]}`), &args); err != nil {
		t.Fatal(err)
	}
	changes := Normalize("multi_edit", args)
	if len(changes) != 3 || changes[0].Operation != "create" || changes[1].Operation != "delete" || changes[2].Operation != "reparent" {
		t.Fatalf("changes=%+v", changes)
	}
	if strings.Join(changes[0].Properties, ",") != "Name,Size" {
		t.Fatal(changes[0].Properties)
	}
	body, _ := json.Marshal(changes)
	if strings.Contains(string(body), "secret-value") {
		t.Fatal("property value retained")
	}
	opaque := Normalize("execute_luau", map[string]any{"code": "workspace.Secret:Destroy()"})
	if len(opaque) != 1 || opaque[0].Target != "" || opaque[0].Operation != "unknown" {
		t.Fatal(opaque)
	}
	if len(Normalize("inspect_instance", args)) != 0 {
		t.Fatal("read-only tool journaled")
	}
	if len(Normalize("wait_job_finished", args)) != 0 {
		t.Fatal("read-only job poll journaled")
	}
	inherited := Normalize("multi_edit", map[string]any{"file_path": "ServerScriptService.Main", "edits": []any{map[string]any{"old_string": "a", "new_string": "b"}}})
	if len(inherited) != 1 || inherited[0].Target != "ServerScriptService.Main" {
		t.Fatalf("batch lost its target: %+v", inherited)
	}
}
