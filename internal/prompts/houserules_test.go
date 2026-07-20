package prompts

import (
	"strings"
	"testing"
)

func TestForRunCarriesHouseRulesFirst(t *testing.T) {
	got := ForRun("You are a Roblox gameplay engineer.", "constitution body")
	if !strings.HasPrefix(got, HouseRules) {
		t.Fatalf("house rules must lead the prompt, got %q", got)
	}
	for _, want := range []string{"most recent message", "StudioForge is the tool running you", "constitution body", "gameplay engineer"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

func TestForRunSkipsEmptyParts(t *testing.T) {
	got := ForRun("", "")
	if got != HouseRules {
		t.Fatalf("expected bare house rules, got %q", got)
	}
	if strings.Contains(ForRun("persona", ""), "Project context") {
		t.Fatal("empty project context must not emit a section")
	}
}

func TestForRunExactOutputBareHouseRules(t *testing.T) {
	got := ForRun("", "")
	want := HouseRules
	if got != want {
		t.Fatalf("ForRun(\"\", \"\") = %q, want %q", got, want)
	}
}

func TestForRunExactOutputProjectContextOnly(t *testing.T) {
	got := ForRun("", "constitution body")
	want := HouseRules + "\n\n## Project context\n\nconstitution body"
	if got != want {
		t.Fatalf("ForRun(\"\", projectContext) = %q, want %q", got, want)
	}
}

func TestForRunExactOutputPersonaAndProjectContext(t *testing.T) {
	got := ForRun("You are a Roblox gameplay engineer.", "constitution body")
	want := HouseRules + "\n\n## Project context\n\nconstitution body" + "\n\n## Your role\n\nYou are a Roblox gameplay engineer."
	if got != want {
		t.Fatalf("ForRun(persona, projectContext) = %q, want %q", got, want)
	}
}
