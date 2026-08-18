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
		"Make the ScreenGui scale properly",
		"Give the coin counter a UIListLayout",
		"redesign the main screen",
		"add a health bar above each player",
		"Сделай магазин с кнопками",
		"Почини интерфейс инвентаря",
		"Кнопка не нажимается на телефоне",
		"главное меню выглядит криво",
		"добавь экран загрузки",
	} {
		if !TaskTouchesUI(task) {
			t.Errorf("TaskTouchesUI(%q) = false, want true", task)
		}
	}
}

func TestTaskTouchesUILeavesOtherWorkAlone(t *testing.T) {
	for _, task := range []string{
		"Fix the datastore save on leave",
		"The NPC pathfinding walks into walls",
		"Rebuild the place and run a playtest",
		"Add server-side validation to the purchase remote",
		"Почини сохранение прогресса игрока",
		"Оптимизируй спавн мобов",
		"require the module before calling it",
		"build a quick script that logs damage",
		"guide the player to the spawn point",
	} {
		if TaskTouchesUI(task) {
			t.Errorf("TaskTouchesUI(%q) = true, want false", task)
		}
	}
}

// "ui" is a substring of ordinary English words, so it has to match as a word
// rather than anywhere it appears. These are the words that broke a naive
// version of this.
func TestTaskTouchesUIDoesNotMatchOnSubstrings(t *testing.T) {
	for _, task := range []string{
		"build the place",
		"require this module",
		"a quick fix",
		"follow the guide",
		"the fluid simulation is slow",
		"suit up the character",
	} {
		if TaskTouchesUI(task) {
			t.Errorf("TaskTouchesUI(%q) matched on a substring, want false", task)
		}
	}
}

func TestTaskTouchesUIIsCaseInsensitive(t *testing.T) {
	for _, task := range []string{"Fix the HUD", "fix the hud", "FIX THE HUD"} {
		if !TaskTouchesUI(task) {
			t.Errorf("TaskTouchesUI(%q) = false, want true", task)
		}
	}
}

func TestTaskTouchesUIOnEmptyText(t *testing.T) {
	if TaskTouchesUI("") {
		t.Error("empty text touches no UI")
	}
}

func TestRobloxUICoreCoversTheNonNegotiables(t *testing.T) {
	for _, want := range []string{
		"Scale", "UDim2.fromScale", "Offset",
		"UIListLayout", "UIGridLayout", "UIPadding",
		"AnchorPoint",
		"UIAspectRatioConstraint", "UITextSizeConstraint", "TextScaled",
		"phone",
	} {
		if !strings.Contains(RobloxUICore, want) {
			t.Errorf("the compact interface rules must cover %q", want)
		}
	}
	if !strings.Contains(RobloxUICore, RobloxUIReferenceFile) {
		t.Errorf("the compact rules must point at %q", RobloxUIReferenceFile)
	}
}

// The core is meant to be a handful of lines. If it grows into a chapter it
// stops being the thing that can ride along on a run and starts being the thing
// that should have been left in the reference.
func TestRobloxUICoreStaysCompact(t *testing.T) {
	if bullets := strings.Count(RobloxUICore, "\n- "); bullets > 8 {
		t.Errorf("the compact rules have grown to %d bullets; move detail into %s", bullets, RobloxUIReferenceFile)
	}
}

func TestRobloxUIReferenceIsShippedAndSubstantial(t *testing.T) {
	if len(RobloxUIReference) < 2000 {
		t.Fatalf("the reference is %d bytes; it is meant to be the fuller document", len(RobloxUIReference))
	}
	for _, want := range []string{
		"ScreenGui", "SurfaceGui", "BillboardGui",
		"ZIndex", "DisplayOrder",
		"IgnoreGuiInset", "GetGuiInset",
		"TweenService",
		"UIAspectRatioConstraint", "UITextSizeConstraint",
		"Activated",
	} {
		if !strings.Contains(RobloxUIReference, want) {
			t.Errorf("the reference must cover %q", want)
		}
	}
}

func TestRobloxUICoreNamesWhereTheTreeLives(t *testing.T) {
	for _, want := range []string{"StarterGui", "ReplicatedStorage", "LocalScript"} {
		if !strings.Contains(RobloxUICore, want) {
			t.Errorf("the compact rules must say where the tree lives: missing %q", want)
		}
	}
	if !strings.Contains(RobloxUICore, RobloxUICraftFile) {
		t.Errorf("the compact rules must point at %q", RobloxUICraftFile)
	}
}

func TestRobloxUICraftIsShippedAndSubstantial(t *testing.T) {
	if len(RobloxUICraft) < 4000 {
		t.Fatalf("the craft reference is %d bytes; it is meant to be the fuller document", len(RobloxUICraft))
	}
	for _, want := range []string{
		"ViewportFrame", "Path2D", "generate_procedural_model",
		"UIShadow", "ClipsDescendants", "UIGradient",
		"UIScale", "Activated", "MouseEnter",
		"LuckiestGuy", "RobotoMono",
		"StarterGui", "ReplicatedStorage",
	} {
		if !strings.Contains(RobloxUICraft, want) {
			t.Errorf("the craft reference must cover %q", want)
		}
	}
}

// The craft document earns its length by recording behaviour that contradicts a
// reasonable guess. If those disappear it has drifted into restating the docs.
func TestRobloxUICraftKeepsTheCounterintuitiveFindings(t *testing.T) {
	for _, want := range []string{
		"does not fail on a family that does not exist",
		"ignores `UICorner`",
		"do not stack",
	} {
		if !strings.Contains(RobloxUICraft, want) {
			t.Errorf("the craft reference must keep the finding %q", want)
		}
	}
}

// The two references must stay distinct documents rather than one growing a copy
// of the other.
func TestRobloxUIReferencesAreDistinct(t *testing.T) {
	if RobloxUICraft == RobloxUIReference {
		t.Fatal("the two references are the same document")
	}
	if RobloxUICraftFile == RobloxUIReferenceFile {
		t.Fatal("the two references would be written to the same path")
	}
}
