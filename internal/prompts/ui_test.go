package prompts

import (
	"strings"
	"testing"
)

func TestTaskTouchesUIRecognisesInterfaceWork(t *testing.T) {
	for _, task := range []string{
		"Build a shop menu with three tabs",
		"Add a leaderboard to the HUD",
		"The settings dialog is broken on mobile",
		"Сделай магазин с кнопками",
		"Почини интерфейс инвентаря",
	} {
		if !TaskTouchesUI(task) {
			t.Errorf("TaskTouchesUI(%q) = false", task)
		}
	}
}

func TestTaskTouchesUILeavesGameplayWorkAlone(t *testing.T) {
	for _, task := range []string{
		"Fix the datastore save on leave",
		"The NPC pathfinding walks into walls",
		"Почини сохранение прогресса игрока",
		"build a quick script that logs damage",
	} {
		if TaskTouchesUI(task) {
			t.Errorf("TaskTouchesUI(%q) = true", task)
		}
	}
}

func TestRobloxUICoreStaysCompactAndMechanical(t *testing.T) {
	for _, want := range []string{
		"UDim2.fromScale", "UIListLayout", "AnchorPoint", "UIAspectRatioConstraint",
		"UITextSizeConstraint", "StarterGui", "ReplicatedStorage", RobloxUIReferenceFile,
		RobloxUICraftFile,
	} {
		if !strings.Contains(RobloxUICore, want) {
			t.Errorf("RobloxUICore missing %q", want)
		}
	}
	if bullets := strings.Count(RobloxUICore, "\n- "); bullets > 8 {
		t.Fatalf("RobloxUICore grew to %d bullets", bullets)
	}
}

func TestReferencesAreSubstantialAndCraftHasNoGenreVocabulary(t *testing.T) {
	if len(RobloxUIReference) < 2000 || len(RobloxUICraft) < 4000 {
		t.Fatalf("references too short: UI=%d craft=%d", len(RobloxUIReference), len(RobloxUICraft))
	}
	for _, want := range []string{"ScreenGui", "SurfaceGui", "ZIndex", "DisplayOrder", "TweenService", "Activated"} {
		if !strings.Contains(RobloxUIReference, want) {
			t.Errorf("mechanics reference missing %q", want)
		}
	}
	for _, want := range []string{"ViewportFrame", "UIShadow", "ClipsDescendants", "UIScale", "Activated", "MouseEnter"} {
		if !strings.Contains(RobloxUICraft, want) {
			t.Errorf("craft mechanics reference missing %q", want)
		}
	}
	for _, genre := range []string{"Halftone dot fields", "pressed lip", "Title as a ribbon"} {
		if strings.Contains(RobloxUICraft, genre) {
			t.Errorf("genre vocabulary %q leaked into universal craft reference", genre)
		}
	}
}
