package mcp

import (
	"context"
	"os"
	"testing"
	"time"
)

// errorShapeProbe captures what a Luau runtime failure actually reads like, and
// whether TestService is reachable from execute_luau (the load-bearing question
// behind issue #41).
//
// Every failure is caught with pcall and returned as the call's result rather
// than allowed to reach the Output window. That is deliberate: an operator's own
// run may be watching the same console, and a probe that injected errors into it
// could send their agent chasing a fault that was never theirs.
const errorShapeProbe = `
local out = {}
local function add(k, v) table.insert(out, k .. "=" .. tostring(v)) end
local function capture(name, fn)
	local ok, err = pcall(fn)
	add("error." .. name, err)
end

capture("index_nil", function() local t = nil return t.field end)
capture("call_nil", function() local f = nil return f() end)
capture("not_a_member", function() return workspace.NoSuchChildHere.Anything end)
capture("arithmetic", function() return {} + 1 end)
capture("explicit", function() error("something went wrong") end)
capture("explicit_level0", function() error("no position prefix", 0) end)

-- Issue #41: can a test be executed and its result returned structurally?
local okTS, testService = pcall(function() return game:GetService("TestService") end)
add("TestService.reachable", okTS)
if okTS and testService then
	for _, member in ipairs({"Error", "Message", "Check", "Done", "Warn", "Fail", "ExecuteWithStudioRun"}) do
		add("TestService." .. member, (pcall(function() return testService[member] end)))
	end
end

-- Whether a JSON-shaped report can come back through TextResult at all.
local okJSON, encoded = pcall(function()
	return game:GetService("HttpService"):JSONEncode({passed = 2, failed = 1, cases = {"a", "b"}})
end)
add("HttpService.JSONEncode", okJSON)
if okJSON then add("json.sample", encoded) end

add("RunService.IsRunning", game:GetService("RunService"):IsRunning())
add("RunService.IsStudio", game:GetService("RunService"):IsStudio())
return table.concat(out, "\n")
`

// TestRealStudioErrorShape records the shape console.go's parser has to
// recognise, and answers #41's execution-path question in the same pass.
//
//	STUDIOFORGE_REAL_STUDIO=1
func TestRealStudioErrorShape(t *testing.T) {
	if os.Getenv("STUDIOFORGE_REAL_STUDIO") != "1" {
		t.Skip("set STUDIOFORGE_REAL_STUDIO=1 with Roblox Studio open to run the live smoke")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client := connectToOneStudio(ctx, t)

	// Which datamodel is available depends on what Studio is doing right now, and
	// #41 needs to know whether the Play one can be reached at all — a test that
	// asserts against a running game is worthless against the Edit tree. Try both
	// and report which answered.
	answered := false
	// "Play" is rejected outright and "Edit" is unavailable while Studio is
	// playing, so the name for the running game is one of the rest. Which one it
	// is decides whether an agent-authored test can run against a live game at
	// all (#41), so it is worth asking rather than assuming.
	for _, datamodel := range []string{"Play", "Server", "Client", "PlayServer", "PlayClient", "Running", "Game", "server", "client", "Edit"} {
		raw, err := client.Call(ctx, "execute_luau", map[string]any{"code": errorShapeProbe, "datamodel_type": datamodel})
		if err != nil {
			t.Logf("%s datamodel: call failed: %v", datamodel, err)
			continue
		}
		text, err := TextResult(raw)
		if err != nil {
			t.Logf("%s datamodel: %v", datamodel, err)
			continue
		}
		answered = true
		t.Logf("%s datamodel reported:\n%s", datamodel, text)
	}
	if !answered {
		t.Fatal("neither datamodel answered; this run establishes nothing")
	}
}
