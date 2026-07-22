package catalog

import "testing"

func testModels() []Model {
	return []Model{
		{ID: "vendor/agent-paid", SupportedParameters: []string{"tools"}, Architecture: Architecture{OutputModalities: []string{"text"}}, Pricing: Pricing{Prompt: "0.000001", Completion: "0.000002"}},
		{ID: "vendor/agent-free", SupportedParameters: []string{"tools"}, Architecture: Architecture{OutputModalities: []string{"text"}}, Pricing: Pricing{Prompt: "0", Completion: "0"}},
		{ID: "vendor/no-tools", Architecture: Architecture{OutputModalities: []string{"text"}}, Pricing: Pricing{Prompt: "0", Completion: "0"}},
	}
}

func TestAgentModelsExcludesModelsWithoutTools(t *testing.T) {
	got := AgentModels(testModels())
	if len(got) != 2 {
		t.Fatalf("agentModels=%d, want 2", len(got))
	}
	for _, m := range got {
		if m.ID == "vendor/no-tools" {
			t.Fatalf("vendor/no-tools should be excluded, got %+v", got)
		}
	}
}

func TestFreeModelsRequiresAgentCompatibleAndFree(t *testing.T) {
	got := FreeModels(testModels())
	if len(got) != 1 || got[0].ID != "vendor/agent-free" {
		t.Fatalf("freeModels=%+v, want only vendor/agent-free", got)
	}
}

func TestFindByID(t *testing.T) {
	models := testModels()
	found, ok := FindByID(models, "vendor/agent-paid")
	if !ok || found.ID != "vendor/agent-paid" {
		t.Fatalf("found=%+v ok=%v", found, ok)
	}
	missing, ok := FindByID(models, "vendor/does-not-exist")
	if ok {
		t.Fatalf("expected ok=false for missing id, got %+v", missing)
	}
	if missing.ID != "" {
		t.Fatalf("expected zero-value Model on miss, got %+v", missing)
	}
}
