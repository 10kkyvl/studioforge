package prompts

import (
	_ "embed"
	"strings"
	"unicode"
)

// RobloxUIReferenceFile is where the full interface reference is written inside
// a project, and the path the core rules point the agent at. It sits in
// `.agent/` beside the operator's own context files, but is deliberately not one
// of the two files projects.LoadContext reads: it is there to be opened when it
// is needed, not carried in every prompt.
const RobloxUIReferenceFile = ".agent/roblox-ui.md"

// RobloxUIReference is the fuller interface reference, shipped with StudioForge
// rather than left to whatever the model happens to know. It is written into a
// project so that every provider can reach it the same way — Claude through its
// own Read tool, OpenRouter and NVIDIA through agenttools' workspace-sandboxed
// read_file — instead of depending on the operator's local Claude install, which
// OpenRouter and NVIDIA runs do not have at all.
//
//go:embed reference/roblox-ui.md
var RobloxUIReference string

// RobloxUICraftFile is where the craft reference is written inside a project.
// It sits beside RobloxUIReferenceFile for the same reason: read on demand, not
// carried in every prompt.
const RobloxUICraftFile = ".agent/roblox-ui-craft.md"

// RobloxUICraft is the second interface reference: what separates an interface
// that reads as a shipped game from one that reads as a mockup. It is a separate
// document rather than more of roblox-ui.md because the two answer different
// questions — that one is how to make an interface that survives a phone, this
// one is how to make it look deliberate — and an agent that only needs the first
// should not have to read past the second to find it.
//
// Much of it records engine behaviour that contradicts a reasonable guess:
// Font.new falling back silently on a missing family, ClipsDescendants ignoring
// UICorner, two UIStroke children merging rather than stacking. Those cost a run
// nothing to get wrong and produce no error, so they are worth shipping rather
// than leaving to be rediscovered.
//
//go:embed reference/roblox-ui-craft.md
var RobloxUICraft string

// RobloxUICore is the part of the interface rules that is not worth making the
// agent fetch: the handful of defaults that decide whether what it builds
// survives a screen that is not the one it was looking at. None of these
// failures produce an error — the console stays clean, the playtest passes, and
// the operator finds out by opening the game on a phone — so an agent that never
// reads the reference still has to get them right.
//
// It is carried only by runs whose work touches UI (see TaskTouchesUI), because
// the house rules earn their length by applying to every run, and this does not.
const RobloxUICore = "## Building Roblox interfaces\n\n" +
	"- Most Roblox sessions are on a phone. Size and position with the Scale half of `UDim2` (`UDim2.fromScale`), and keep Offset for things that must stay a fixed number of pixels — borders, padding, a stroke. A frame built with `UDim2.new(0, 200, 0, 50)` is correct only on the screen it was measured on.\n" +
	"- Lay out with `UIListLayout`, `UIGridLayout` and `UIPadding` instead of computing a `Position` per element. Hand-placed positions stop being right the moment the parent changes size.\n" +
	"- Set `AnchorPoint` when you centre or right-align something: `AnchorPoint = Vector2.new(0.5, 0.5)` with `Position = UDim2.fromScale(0.5, 0.5)` is centred at any size, an offset computed from half the parent's width is not.\n" +
	"- Constrain what stretching would ruin: `UIAspectRatioConstraint` on buttons and icons so they don't become rectangles, and `UITextSizeConstraint` wherever you set `TextScaled`, or text collapses on a phone and balloons on a monitor.\n" +
	"- Check your work at more than one shape before calling it done — a tall phone and a wide monitor disagree about almost every layout mistake.\n" +
	"- Build the tree as real Instances under `StarterGui` rather than having a `LocalScript` construct it on join: StarterGui is already cloned into every player's `PlayerGui`, and a scripted tree is one nobody can select or edit in Explorer. Script only what changes at runtime — text, bars, colours that follow rarity, lists whose length depends on data. Repeated pieces go in `ReplicatedStorage` as templates to clone, never left loose in `Workspace`.\n" +
	"- Two references ship with the project. `" + RobloxUIReferenceFile + "` covers the mechanics: surface types, layering, the topbar and safe area, animation. `" + RobloxUICraftFile + "` covers making it look built rather than generated: icons through `ViewportFrame`, which font families actually load, native `UIShadow`, hover and press motion, and the checklist to run before calling a screen done. Read both before building anything larger than a single frame."

// uiWords are whole words that mean interface work on their own.
var uiWords = map[string]bool{
	"ui": true, "gui": true, "hud": true, "screengui": true, "surfacegui": true,
	"billboardgui": true, "udim2": true, "uilistlayout": true, "uigridlayout": true,
	"uipadding": true, "anchorpoint": true, "textscaled": true, "viewportframe": true,
	"menu": true, "hotbar": true, "topbar": true, "tooltip": true, "popup": true,
	"худ": true, "гуи": true, "меню": true,
}

// uiStems are word beginnings that mean interface work. They carry the Russian
// side, where the same noun appears in half a dozen inflections ("кнопка",
// "кнопки", "кнопкой"), and the English words that inflect too ("button",
// "buttons"). A stem must be long enough not to swallow an unrelated word:
// "интерфейс" is safe, "окн" would also match "окно" in "окно Studio", which is
// close enough to interface work to be worth the occasional extra section.
var uiStems = []string{
	"button", "dialog", "leaderboard", "inventor", "shopfront", "screen",
	"interface", "layout", "widget", "panel", "overlay", "scoreboard",
	"кнопк", "интерфейс", "экран", "окно", "окна", "магазин", "инвентар",
	"лидерборд", "панел", "менюшк", "всплыва", "вёрстк", "верстк", "разметк",
}

// uiPhrases are multi-word forms where neither word alone means interface work.
var uiPhrases = []string{
	"shop", "settings", "main screen", "start screen", "loading screen",
	"health bar", "progress bar", "text label", "quest log",
	"главное меню", "полоса здоровья", "строка прогресса",
}

// TaskTouchesUI reports whether a run's task text is about building or changing
// an interface, and so should carry RobloxUICore.
//
// It is a keyword match, and it is meant to be: the alternative — asking a model
// to classify the task before the run starts — spends a round trip and a
// judgment call on something a word list gets right nearly all the time. It errs
// toward yes. A run that carries six lines it did not need costs a little
// context; a run that builds a shop menu in fixed pixels costs the operator a
// rebuild after they open the game on a phone.
func TaskTouchesUI(text string) bool {
	lowered := strings.ToLower(text)
	for _, phrase := range uiPhrases {
		if strings.Contains(lowered, phrase) {
			return true
		}
	}
	for _, token := range strings.FieldsFunc(lowered, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if uiWords[token] {
			return true
		}
		for _, stem := range uiStems {
			if strings.HasPrefix(token, stem) {
				return true
			}
		}
	}
	return false
}
