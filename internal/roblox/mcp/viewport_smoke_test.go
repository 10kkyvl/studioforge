package mcp

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// viewportProbe asks a live Studio the load-bearing question behind issue #43:
// can the viewport a playtest screenshot is taken at be driven from here?
//
// It tries every route that could plausibly work, in order of how much it would
// give us, and reports what each one answers rather than assuming. The point is
// to settle the question against a real Studio rather than against memory of
// the API — the same reason the interface reference has its own probe.
const viewportProbe = `
local out = {}
local function add(k, v) table.insert(out, k .. "=" .. tostring(v)) end
local function try(name, fn)
	local ok, err = pcall(fn)
	add(name, ok and "yes" or ("no: " .. tostring(err):gsub("\n", " "):sub(1, 90)))
end

local camera = workspace.CurrentCamera
add("camera.present", camera ~= nil)
if camera then
	add("camera.ViewportSize", tostring(camera.ViewportSize))
	-- If this is writable, a playtest could simply set the shape it wants.
	try("camera.ViewportSize.writable", function()
		camera.ViewportSize = Vector2.new(390, 844)
	end)
end

-- The plugin global is what a Studio plugin would use to reach Studio-only
-- surfaces; execute_luau may or may not run with it.
add("plugin.available", plugin ~= nil)

-- Services that could plausibly own device emulation.
for _, name in ipairs({"GuiService", "StudioService", "RunService", "UserInputService"}) do
	local ok, service = pcall(function() return game:GetService(name) end)
	add("service." .. name, ok)
	if ok and service then
		for _, member in ipairs({
			"SetEmulatedDeviceProfile", "EmulatedDeviceProfile", "EmulatedDevice",
			"SetEmulatedDevice", "DeviceProfile", "SelectedDevice", "EmulationEnabled",
		}) do
			local hasIt = pcall(function() return service[member] end)
			if hasIt then add("member." .. name .. "." .. member, "present") end
		end
	end
end

-- Studio's own settings tree, the other place an emulation switch could live.
try("settings.Studio", function()
	local studio = settings():GetService("Studio")
	return studio ~= nil
end)

add("viewport.aspect", camera and (camera.ViewportSize.X / camera.ViewportSize.Y) or "n/a")
return table.concat(out, "\n")
`

// TestRealStudioViewportEmulation settles issue #43's feasibility question
// against a Studio that is actually open.
//
//	STUDIOFORGE_REAL_STUDIO=1
func TestRealStudioViewportEmulation(t *testing.T) {
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
	raw, err := client.Call(ctx, "execute_luau", map[string]any{"code": viewportProbe, "datamodel_type": "Edit"})
	if err != nil {
		t.Fatal(err)
	}
	text, err := TextResult(raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("live Studio reported:\n%s", text)
	if !strings.Contains(text, "camera.present") {
		t.Fatalf("the probe returned nothing usable: %q", text)
	}
}
