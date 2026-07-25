package prompts

import (
	"strings"
	"testing"
)

func TestForRunCarriesHouseRulesFirst(t *testing.T) {
	got := ForRun(Spec{Persona: "You are a Roblox gameplay engineer.", ProjectContext: "constitution body"})
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
	got := ForRun(Spec{})
	if got != HouseRules {
		t.Fatalf("expected bare house rules, got %q", got)
	}
	if strings.Contains(ForRun(Spec{Persona: "persona"}), "Project context") {
		t.Fatal("empty project context must not emit a section")
	}
	if strings.Contains(ForRun(Spec{Persona: "persona"}), "Relevant project memory") {
		t.Fatal("empty memory must not emit a section")
	}
	if strings.Contains(ForRun(Spec{Persona: "persona", Memory: "   "}), "Relevant project memory") {
		t.Fatal("blank memory must not emit a section")
	}
}

func TestForRunExactOutputBareHouseRules(t *testing.T) {
	got := ForRun(Spec{})
	if got != HouseRules {
		t.Fatalf("ForRun(Spec{}) = %q, want %q", got, HouseRules)
	}
}

func TestForRunExactOutputProjectContextOnly(t *testing.T) {
	got := ForRun(Spec{ProjectContext: "constitution body"})
	want := HouseRules + "\n\n## Project context\n\nconstitution body"
	if got != want {
		t.Fatalf("ForRun(projectContext) = %q, want %q", got, want)
	}
}

func TestForRunExactOutputPersonaAndProjectContext(t *testing.T) {
	got := ForRun(Spec{Persona: "You are a Roblox gameplay engineer.", ProjectContext: "constitution body"})
	want := HouseRules + "\n\n## Your role\n\nYou are a Roblox gameplay engineer." + "\n\n## Project context\n\nconstitution body"
	if got != want {
		t.Fatalf("ForRun(persona, projectContext) = %q, want %q", got, want)
	}
}

// The ordering is the whole of issue #32: prompt caching is a prefix match, so
// the parts have to run from the ones that never change to the ones that change
// on every run.
func TestForRunEmitsPartsFromStableToVolatile(t *testing.T) {
	got := ForRun(Spec{
		Persona:        "persona body",
		ProjectContext: "context body",
		Memory:         "memory body",
		Questions:      QuestionFence,
		UI:             true,
	})
	order := []string{
		"## How you operate",
		"## Asking closed questions",
		"## Your role",
		"## Project context",
		"## Building Roblox interfaces",
		"## Relevant project memory",
	}
	previous := -1
	for _, heading := range order {
		at := strings.Index(got, heading)
		if at < 0 {
			t.Fatalf("missing section %q in %q", heading, got)
		}
		if at < previous {
			t.Fatalf("section %q is out of order; want %v", heading, order)
		}
		previous = at
	}
}

// Two runs of the same agent on the same project differ only in their memory
// selection, so everything ahead of the memory block has to be byte-identical
// or none of it can ever be cached.
func TestForRunKeepsAnIdenticalPrefixWhenOnlyMemoryDiffers(t *testing.T) {
	base := Spec{Persona: "persona body", ProjectContext: "context body", Questions: QuestionTool}
	first, second := base, base
	first.Memory = "- the shop uses a ProfileService store"
	second.Memory = "- the lobby teleporter is server-authoritative"

	firstPrompt, secondPrompt := ForRun(first), ForRun(second)
	firstAt := strings.Index(firstPrompt, "## Relevant project memory")
	secondAt := strings.Index(secondPrompt, "## Relevant project memory")
	if firstAt < 0 || secondAt < 0 {
		t.Fatalf("both prompts must carry a memory block; got %d and %d", firstAt, secondAt)
	}
	if firstAt != secondAt {
		t.Fatalf("memory block starts at %d and %d; the prefix ahead of it is not the same length", firstAt, secondAt)
	}
	if firstPrompt[:firstAt] != secondPrompt[:secondAt] {
		t.Fatalf("prefix before memory differs:\n%q\n%q", firstPrompt[:firstAt], secondPrompt[:secondAt])
	}
	if firstPrompt == secondPrompt {
		t.Fatal("the two prompts should still differ after the memory block")
	}
}

func TestForRunMemoryIsItsOwnSectionNotFoldedIntoProjectContext(t *testing.T) {
	got := ForRun(Spec{ProjectContext: "context body", Memory: "- remembered thing"})
	contextAt := strings.Index(got, "## Project context")
	memoryAt := strings.Index(got, "## Relevant project memory")
	if contextAt < 0 || memoryAt < 0 {
		t.Fatalf("both sections must be present, got %q", got)
	}
	if memoryAt < contextAt {
		t.Fatal("memory must come after project context, not inside or before it")
	}
	if strings.Contains(got[contextAt:memoryAt], "remembered thing") {
		t.Fatal("memory content leaked into the project context section")
	}
}

// Issue #35: the house rules said nothing about how much to write or how far to
// go beyond the request.
func TestHouseRulesCoverScopeAndLength(t *testing.T) {
	if !strings.Contains(HouseRules, "## Scope and length") {
		t.Fatal("house rules must carry a scope-and-length section")
	}
	for _, want := range []string{
		"at the scope it was asked at",
		"Don't add abstractions",
		"Finish the whole task",
		"Between tool calls",
		"Lead your final message with the outcome",
	} {
		if !strings.Contains(HouseRules, want) {
			t.Fatalf("scope-and-length section is missing %q", want)
		}
	}
}

// The house rules are the byte-identical head of every prompt. Anything that
// varies per run belongs in a later part, and the Studio section moving out is
// the case this guards.
func TestHouseRulesNameNoStudioTool(t *testing.T) {
	for _, tool := range []string{"generate_mesh", "insert_asset", "search_asset", "start_stop_play", "screen_capture", "get_console_output", "wait_job_finished"} {
		if strings.Contains(HouseRules, tool) {
			t.Fatalf("house rules must not name the Studio tool %q; it is composed from the run's grant", tool)
		}
	}
}

func TestQuestionSectionMatchesTheChannelTheRunActuallyHas(t *testing.T) {
	toolPrompt := ForRun(Spec{Questions: QuestionTool})
	if !strings.Contains(toolPrompt, "studioforge_question tool") {
		t.Fatalf("a run with the tool must be told to use it, got %q", toolPrompt)
	}
	if strings.Contains(toolPrompt, "```studioforge-question") {
		t.Fatal("a run with the tool must not also be taught the text fence")
	}
	if strings.Contains(toolPrompt, "info-string") {
		t.Fatal("the tool path must carry no format instructions at all")
	}

	fencePrompt := ForRun(Spec{Questions: QuestionFence})
	if !strings.Contains(fencePrompt, "```studioforge-question") {
		t.Fatal("a run without the tool must still be taught the fence")
	}
	for _, want := range []string{"info-string", "Send nothing else in that message"} {
		if !strings.Contains(fencePrompt, want) {
			t.Fatalf("the fence path must keep its defences; missing %q", want)
		}
	}

	if len(fencePrompt) <= len(toolPrompt) {
		t.Fatal("the tool path exists to be shorter than the fence path")
	}
}

// Issue #34: a subagent's turn ends inside its parent's run and the operator's
// answer resumes the parent, so a subagent cannot receive an answer to a
// question it asked. It must not be told it can ask one.
func TestNoQuestionsEmitsNoQuestionSection(t *testing.T) {
	got := ForRun(Spec{Persona: "subagent persona", ProjectContext: "context body", Questions: NoQuestions})
	for _, unwanted := range []string{"Asking closed questions", "studioforge-question", "studioforge_question"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("a prompt with no question channel must not mention %q", unwanted)
		}
	}
	if !strings.Contains(got, "context body") {
		t.Fatal("a subagent still gets the project's standing context")
	}
}

func TestUISectionOnlyWhenAskedFor(t *testing.T) {
	if strings.Contains(ForRun(Spec{Persona: "persona"}), "Building Roblox interfaces") {
		t.Fatal("the interface rules must not ride on every run")
	}
	got := ForRun(Spec{Persona: "persona", UI: true})
	if !strings.Contains(got, "Building Roblox interfaces") {
		t.Fatal("a UI run must carry the interface rules")
	}
}
