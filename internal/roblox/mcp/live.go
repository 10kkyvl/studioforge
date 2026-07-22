package mcp

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type LiveGrant struct {
	Client       *Client
	AllowedTools []string
	Context      string
	Notice       string
	Release      func()
}

func (p *Provisioner) ProvisionLive(ctx context.Context, permissionProfile string, target Target) LiveGrant {
	tools := AllowedTools(permissionProfile)
	if len(tools) == 0 {
		return LiveGrant{Notice: fmt.Sprintf("Studio MCP withheld: permission profile %q grants no Studio tools", permissionProfile)}
	}
	override := ""
	if p.Override != nil {
		override = p.Override()
	}
	launch, err := DetectLauncher(override)
	if err != nil {
		return LiveGrant{}
	}
	transport, err := p.dialLive(ctx, launch)
	if err != nil {
		return LiveGrant{Notice: "Studio MCP withheld: " + err.Error()}
	}
	client := NewClient(transport)
	instances, state, err := p.listWithAttach(ctx, client)
	if errors.Is(err, errWSHostUnreachable) {
		_ = client.Close()
		return LiveGrant{Notice: hostTakenNotice}
	}
	if err != nil {
		_ = client.Close()
		return LiveGrant{Notice: "Studio MCP withheld: " + err.Error()}
	}
	instances, state, notice := p.selectForTargetLive(ctx, client, target, instances, state)
	if notice != "" {
		_ = client.Close()
		return LiveGrant{Notice: notice}
	}
	if len(instances) == 0 {
		if p.blocked(ctx) {
			_ = client.Close()
			return LiveGrant{Notice: hostTakenNotice}
		}
		_ = client.Close()
		return LiveGrant{}
	}
	if err := client.SelectStudio(ctx, instances[0].ID); err != nil {
		_ = client.Close()
		return LiveGrant{Notice: "Studio MCP withheld: pinning the matched Studio instance failed: " + err.Error()}
	}
	if raw, callErr := client.Call(ctx, "get_studio_state", nil); callErr == nil {
		state = studioStateText(raw)
	}
	return LiveGrant{
		Client:       client,
		AllowedTools: tools,
		Context:      state,
		Release:      func() { _ = client.Close() },
	}
}

func (p *Provisioner) dialLive(ctx context.Context, launch LaunchConfig) (Transport, error) {
	type outcome struct {
		transport Transport
		err       error
	}
	done := make(chan outcome, 1)
	go func() {
		transport, err := p.dial(ctx, launch)
		done <- outcome{transport, err}
	}()
	waitCtx, cancel := context.WithTimeout(ctx, p.timeout())
	defer cancel()
	select {
	case r := <-done:
		return r.transport, r.err
	case <-waitCtx.Done():
		go func() {
			if r := <-done; r.transport != nil {
				_ = r.transport.Close()
			}
		}()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("Studio MCP launcher did not respond within %s", p.timeout())
	}
}

func (p *Provisioner) selectForTargetLive(ctx context.Context, client *Client, target Target, instances []Instance, state string) ([]Instance, string, string) {
	if target.PlaceName == "" {
		if len(instances) > 1 {
			return nil, "", fmt.Sprintf("Studio MCP withheld: %d Studio instances are open and StudioForge cannot pin one for the agent's own MCP connection; leave a single Studio open", len(instances))
		}
		return instances, state, ""
	}

	matched := matching(instances, target.PlaceName)
	switch {
	case len(matched) == 1:
		return matched, state, ""
	case len(matched) > 1:
		return nil, "", ambiguousMatchNotice(len(matched), target.PlaceName)
	}

	if len(instances) > 0 {
		return nil, "", mismatchNotice(instances, target.PlaceName)
	}

	if p.blocked(ctx) {
		return nil, "", hostTakenNotice
	}

	if target.Open == nil || !p.autoOpen() {
		return nil, "", ""
	}
	if err := target.Open(ctx); err != nil {
		return nil, "", "Studio MCP withheld: opening this project's place failed: " + err.Error()
	}
	opened, state, err := p.waitForPlaceLive(ctx, client, target.PlaceName)
	if err != nil {
		return nil, "", "Studio MCP withheld: " + err.Error()
	}
	if len(opened) != 1 {
		return nil, "", fmt.Sprintf("Studio MCP withheld: %s did not finish opening within %s; the run continues without Studio", target.PlaceName, openWait)
	}
	return opened, state, ""
}

func (p *Provisioner) waitForPlaceLive(ctx context.Context, client *Client, placeName string) ([]Instance, string, error) {
	ctx, cancel := context.WithTimeout(ctx, openWait)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		instances, err := client.ListStudios(ctx)
		if err == nil {
			if matched := matching(instances, placeName); len(matched) > 0 {
				state := ""
				if raw, callErr := client.Call(ctx, "get_studio_state", nil); callErr == nil {
					state = studioStateText(raw)
				}
				return matched, state, nil
			}
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return nil, "", nil
		}
	}
}

func (p *Provisioner) listWithAttach(ctx context.Context, client *Client) ([]Instance, string, error) {
	listCtx, cancel := context.WithTimeout(ctx, p.timeout())
	defer cancel()
	var attach <-chan time.Time
	instances, err := client.ListStudios(listCtx)
	for notConnected(err) {
		if !p.blocked(ctx) {
			return nil, "", nil
		}
		if attach == nil {
			window := p.attachWindow
			if window <= 0 {
				window = attachWait
			}
			attach = time.After(window)
		}
		retry := p.retryEvery
		if retry <= 0 {
			retry = time.Second
		}
		select {
		case <-time.After(retry):
		case <-attach:
			return nil, "", errWSHostUnreachable
		case <-listCtx.Done():
			return nil, "", errWSHostUnreachable
		}
		instances, err = client.ListStudios(listCtx)
	}
	if err != nil {
		if IsMethodNotFound(err) {
			return nil, "", fmt.Errorf("Studio MCP exposes no instance listing; update Roblox Studio")
		}
		return nil, "", err
	}
	state := ""
	if len(instances) == 1 {
		if raw, callErr := client.Call(listCtx, "get_studio_state", nil); callErr == nil {
			state = studioStateText(raw)
		}
	}
	return instances, state, nil
}
