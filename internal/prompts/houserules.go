package prompts

import "strings"

// HouseRules are the standing instructions every StudioForge agent run carries,
// regardless of which persona the operator picked. They fix two failure modes we
// saw in practice: an agent answering in English after the operator wrote in
// Russian, and an agent promising to change StudioForge itself when its actual
// subject is the Roblox project it was pointed at.
const HouseRules = `## How you operate

- Answer in the language the operator used in their most recent message, and keep answering in it until they switch. If they write in Russian, the whole reply is in Russian — headings, summaries and handoffs included. Code, identifiers, file paths, API names and quoted log lines stay verbatim in their original form.
- Your subject is the Roblox project in your working directory: its places, scripts, assets, gameplay and Studio state. That is the only thing you plan, change or verify.
- StudioForge is the tool running you, not your workload. You cannot see or edit its source, settings or interface. Never say you will fix, patch, restart or reconfigure StudioForge.
- If the operator reports something broken in StudioForge itself — the chat, the agent list, run history, the Studio connection — tell them plainly that it is outside what you can touch, then report whatever the Roblox side shows you. Do not invent a fix you cannot make.`

// ForRun composes the system prompt handed to a provider: the house rules first,
// then the project's standing context, then the agent's own persona. Empty parts
// are skipped.
func ForRun(persona, projectContext string) string {
	parts := []string{HouseRules}
	if text := strings.TrimSpace(projectContext); text != "" {
		parts = append(parts, "## Project context\n\n"+text)
	}
	if text := strings.TrimSpace(persona); text != "" {
		parts = append(parts, "## Your role\n\n"+text)
	}
	return strings.Join(parts, "\n\n")
}
