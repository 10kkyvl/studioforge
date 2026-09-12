package mcp

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// TestRealSnapshotResearch is a read-only cost probe for #18. It never starts
// play mode, creates instances, writes attributes, or reads script source.
func TestRealSnapshotResearch(t *testing.T) {
	if os.Getenv("STUDIOFORGE_REAL_STUDIO") != "1" {
		t.Skip("set STUDIOFORGE_REAL_STUDIO=1 with one test place open")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	p := &Provisioner{Dir: t.TempDir()}
	grant := p.ProvisionLive(ctx, "workspace-write", Target{})
	if grant.Client == nil {
		t.Fatalf("snapshot measurement unavailable: %s", grant.Notice)
	}
	defer grant.Release()
	const code = `local started=os.clock()
local root=workspace
local nodes={}
local queue={root}
local cursor=1
local truncated=false
while cursor<=#queue do
 local item=queue[cursor]
 cursor+=1
 local row={path=item:GetFullName(),class=item.ClassName,name=item.Name,properties={}}
 if item:IsA("BasePart") then
  row.properties.Size={item.Size.X,item.Size.Y,item.Size.Z}
  row.properties.CFrame={item.CFrame:GetComponents()}
  row.properties.Color={item.Color.R,item.Color.G,item.Color.B}
  row.properties.Anchored=item.Anchored
  row.properties.CanCollide=item.CanCollide
 end
 table.insert(nodes,row)
 for _,child in ipairs(item:GetChildren()) do
  if #queue<1000 then table.insert(queue,child) else truncated=true end
 end
end
local encoded=game:GetService("HttpService"):JSONEncode(nodes)
return game:GetService("HttpService"):JSONEncode({nodes=#nodes,bytes=#encoded,captureSeconds=os.clock()-started,truncated=truncated})`
	for i := 0; i < 5; i++ {
		start := time.Now()
		result, err := grant.Client.Call(ctx, "execute_luau", map[string]any{"code": code, "datamodel": "Edit"})
		if err != nil {
			t.Fatal(err)
		}
		text, err := TextResult(result)
		if err != nil {
			t.Fatal(err)
		}
		var metrics map[string]any
		if err := json.Unmarshal([]byte(text), &metrics); err != nil {
			t.Fatalf("invalid probe output %q: %v", text, err)
		}
		t.Logf("sample=%d roundTrip=%s metrics=%s", i+1, time.Since(start), text)
	}
}
