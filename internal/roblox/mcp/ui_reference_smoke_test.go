package mcp

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// uiReferenceProbe checks, inside a live Studio, every claim
// internal/prompts/reference/roblox-ui.md makes that could be wrong: the
// property defaults it states outright, the classes and events it tells an
// agent to use, and the layout behaviour it promises.
//
// The layout half runs twice, in two stages sized to a desktop and a phone
// aspect ratio, because that is the whole point of the rules — a mistake that
// is invisible at 16:9 is what the operator finds when they open the game on a
// phone. Building both stages as fixed-size frames inside one ScreenGui tests
// the same thing the device emulator would, without needing anyone to drive the
// emulator by hand.
const uiReferenceProbe = `
local out = {}
local function add(k, v) table.insert(out, k .. "=" .. tostring(v)) end

-- Property defaults the reference states outright.
local sg = Instance.new("ScreenGui")
add("ScreenGui.IgnoreGuiInset", sg.IgnoreGuiInset)
add("ScreenGui.ResetOnSpawn", sg.ResetOnSpawn)
add("ScreenGui.ZIndexBehavior", sg.ZIndexBehavior.Name)
local list = Instance.new("UIListLayout")
add("UIListLayout.SortOrder", list.SortOrder.Name)

-- Classes and members the reference tells an agent to reach for.
for _, class in ipairs({"ScreenGui","SurfaceGui","BillboardGui","UIListLayout","UIGridLayout",
        "UIPadding","UIAspectRatioConstraint","UITextSizeConstraint","UISizeConstraint",
        "UICorner","UIStroke","UIGradient","UIScale","UIFlexItem","CanvasGroup"}) do
    add("class." .. class, (pcall(Instance.new, class)))
end
local button = Instance.new("TextButton")
add("TextButton.Activated", (pcall(function() return button.Activated end)))
add("UDim2.fromScale", (pcall(function() return UDim2.fromScale(0.5, 0.5) end)))
add("UDim2.fromOffset", (pcall(function() return UDim2.fromOffset(10, 10) end)))
local okInset, top = pcall(function() return game:GetService("GuiService"):GetGuiInset() end)
add("GuiService.GetGuiInset", okInset)
if okInset then add("GuiService.inset.Y", top.Y) end
local surface = Instance.new("SurfaceGui")
add("SurfaceGui.PixelsPerStud", (pcall(function() return surface.PixelsPerStud end)))
local billboard = Instance.new("BillboardGui")
add("BillboardGui.MaxDistance", (pcall(function() return billboard.MaxDistance end)))

-- Layout behaviour, at a desktop and a phone aspect ratio.
local host = game:GetService("CoreGui")
local existing = host:FindFirstChild("StudioForgeUIProbe")
if existing then existing:Destroy() end
local screen = Instance.new("ScreenGui")
screen.Name = "StudioForgeUIProbe"
screen.Parent = host

local function stage(label, width, height)
    local root = Instance.new("Frame")
    root.Name = label
    root.Size = UDim2.fromOffset(width, height)
    root.BackgroundTransparency = 1
    root.Parent = screen

    -- Centred with AnchorPoint + Scale, the way the reference says to centre.
    local centred = Instance.new("Frame")
    centred.Name = "centred"
    centred.AnchorPoint = Vector2.new(0.5, 0.5)
    centred.Position = UDim2.fromScale(0.5, 0.5)
    centred.Size = UDim2.fromScale(0.5, 0.5)
    centred.Parent = root

    -- A square icon, with and without an aspect ratio constraint.
    local free = Instance.new("Frame")
    free.Name = "free"
    free.Size = UDim2.fromScale(0.3, 0.3)
    free.Parent = root
    local locked = Instance.new("Frame")
    locked.Name = "locked"
    locked.Size = UDim2.fromScale(0.3, 0.3)
    locked.Parent = root
    local ratio = Instance.new("UIAspectRatioConstraint")
    ratio.AspectRatio = 1
    ratio.Parent = locked

    -- TextScaled, with and without the size constraint the reference pairs it with.
    local loose = Instance.new("TextLabel")
    loose.Name = "loose"
    loose.Size = UDim2.fromScale(0.4, 0.2)
    loose.Text = "Shop"
    loose.TextScaled = true
    loose.Parent = root
    local bounded = Instance.new("TextLabel")
    bounded.Name = "bounded"
    bounded.Size = UDim2.fromScale(0.4, 0.2)
    bounded.Text = "Shop"
    bounded.TextScaled = true
    bounded.Parent = root
    local textSize = Instance.new("UITextSizeConstraint")
    textSize.MinTextSize = 14
    textSize.MaxTextSize = 32
    textSize.Parent = bounded

    -- Offset sizing, the failure the reference leads with.
    local fixed = Instance.new("Frame")
    fixed.Name = "fixed"
    fixed.Size = UDim2.new(0, 200, 0, 50)
    fixed.Parent = root

    return root, label
end

local stages = {}
stages[1] = {stage("desktop", 1920, 1080)}
stages[2] = {stage("phone", 390, 844)}

for _ = 1, 3 do game:GetService("RunService").Heartbeat:Wait() end

for _, entry in ipairs(stages) do
    local root, label = entry[1], entry[2]
    local size = root.AbsoluteSize
    local centred = root.centred
    -- Where is the middle of the centred frame, relative to the stage's middle?
    local middle = centred.AbsolutePosition + centred.AbsoluteSize / 2
    local want = root.AbsolutePosition + size / 2
    add(label .. ".centredOffsetX", math.abs(middle.X - want.X))
    add(label .. ".centredOffsetY", math.abs(middle.Y - want.Y))
    add(label .. ".freeRatio", root.free.AbsoluteSize.X / root.free.AbsoluteSize.Y)
    add(label .. ".lockedRatio", root.locked.AbsoluteSize.X / root.locked.AbsoluteSize.Y)
    add(label .. ".looseTextY", root.loose.TextBounds.Y)
    add(label .. ".boundedTextY", root.bounded.TextBounds.Y)
    add(label .. ".fixedFractionX", root.fixed.AbsoluteSize.X / size.X)
end
screen:Destroy()
return table.concat(out, "\n")
`

// TestRealStudioUIReferenceClaims verifies the interface reference StudioForge
// ships against a Studio that is actually open, which is the only thing that can
// tell a documented default apart from a remembered one.
//
//	STUDIOFORGE_REAL_STUDIO=1
func TestRealStudioUIReferenceClaims(t *testing.T) {
	if os.Getenv("STUDIOFORGE_REAL_STUDIO") != "1" {
		t.Skip("set STUDIOFORGE_REAL_STUDIO=1 with Roblox Studio open to run the live smoke")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	launch, err := DetectLauncher("")
	if err != nil {
		t.Fatal(err)
	}
	transport, err := NewStdioTransport(ctx, launch)
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(transport)
	t.Cleanup(func() { _ = client.Close() })
	if _, err := client.Discover(ctx); err != nil {
		t.Fatal(err)
	}
	// The plugin attaches to a freshly spawned launcher in two steps, so the
	// first listing is routinely empty even with Studio open and registered.
	// Asking once is how this smoke spent three runs reporting no Studio.
	var instances []Instance
	deadline := time.Now().Add(15 * time.Second)
	for {
		instances, err = client.ListStudios(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(instances) > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if len(instances) != 1 {
		t.Skipf("want exactly one open Studio, got %d", len(instances))
	}
	if err := client.SelectStudio(ctx, instances[0].ID); err != nil {
		t.Fatal(err)
	}
	// The launcher's own schema for execute_luau: `code`, plus a required
	// `datamodel_type` out of Edit/Client/Server. Edit is the one that needs no
	// Play session, which is what this probe wants — it is checking layout
	// arithmetic, not gameplay.
	raw, err := client.Call(ctx, "execute_luau", map[string]any{
		"code":           uiReferenceProbe,
		"datamodel_type": "Edit",
	})
	if err != nil {
		t.Fatal(err)
	}
	text, err := TextResult(raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("live Studio reported:\n%s", text)

	facts := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		if key, value, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			facts[key] = value
		}
	}
	if len(facts) == 0 {
		t.Fatalf("the probe returned nothing usable: %q", text)
	}
	for key, want := range map[string]string{
		// The reference tells an agent that the inset default is the one it
		// usually wants, that a runtime-built ScreenGui vanishes on respawn
		// unless ResetOnSpawn is turned off, and — because these two are not
		// what a reader would guess — that ZIndexBehavior and SortOrder must be
		// set explicitly rather than left alone.
		"ScreenGui.IgnoreGuiInset": "false",
		"ScreenGui.ResetOnSpawn":   "true",
		"ScreenGui.ZIndexBehavior": "Global",
		"UIListLayout.SortOrder":   "Name",
		"TextButton.Activated":     "true",
		"UDim2.fromScale":          "true",
		"UDim2.fromOffset":         "true",
		"GuiService.GetGuiInset":   "true",
	} {
		if got, ok := facts[key]; !ok {
			t.Errorf("the probe did not report %s", key)
		} else if got != want {
			t.Errorf("%s = %s, but the reference says %s", key, got, want)
		}
	}
	for key, value := range facts {
		if strings.HasPrefix(key, "class.") && value != "true" {
			t.Errorf("%s does not exist in this Studio, but the reference tells an agent to use it", key)
		}
	}

	number := func(key string) float64 {
		t.Helper()
		value, ok := facts[key]
		if !ok {
			t.Fatalf("the probe did not report %s", key)
		}
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			t.Fatalf("%s = %q, which is not a number: %v", key, value, err)
		}
		return parsed
	}

	// Centring with AnchorPoint and Scale lands exactly on the middle at both
	// shapes. This is the rule an offset computed from half the parent's width
	// is supposed to replace.
	for _, shape := range []string{"desktop", "phone"} {
		for _, axis := range []string{"X", "Y"} {
			if off := number(shape + ".centredOffset" + axis); off > 1 {
				t.Errorf("%s: AnchorPoint centring is %v px off on %s", shape, off, axis)
			}
		}
		// A square is a square at either shape, but only with the constraint.
		if ratio := number(shape + ".lockedRatio"); ratio < 0.99 || ratio > 1.01 {
			t.Errorf("%s: UIAspectRatioConstraint let the ratio drift to %v", shape, ratio)
		}
	}
	// Without the constraint, the same declaration is a different shape on each
	// screen — which is the reason the constraint is in the reference at all.
	desktopFree, phoneFree := number("desktop.freeRatio"), number("phone.freeRatio")
	if desktopFree/phoneFree < 2 {
		t.Errorf("an unconstrained frame was expected to change shape sharply between screens, got %v and %v", desktopFree, phoneFree)
	}
	// TextScaled alone follows the screen; bounded by UITextSizeConstraint it
	// does not.
	if number("desktop.looseTextY") == number("phone.looseTextY") {
		t.Error("TextScaled was expected to render at different sizes on the two screens")
	}
	if number("desktop.boundedTextY") != number("phone.boundedTextY") {
		t.Errorf("UITextSizeConstraint did not hold text steady: %v vs %v", facts["desktop.boundedTextY"], facts["phone.boundedTextY"])
	}
	// The headline claim: a frame sized in pixels covers a tenth of a monitor
	// and half a phone.
	desktopFixed, phoneFixed := number("desktop.fixedFractionX"), number("phone.fixedFractionX")
	if desktopFixed > 0.2 || phoneFixed < 0.4 {
		t.Errorf("offset-only sizing: %.3f of the desktop width and %.3f of the phone width", desktopFixed, phoneFixed)
	}
	t.Logf("offset-only sizing covers %.1f%% of a 1920x1080 screen and %.1f%% of a 390x844 screen",
		desktopFixed*100, phoneFixed*100)
}
