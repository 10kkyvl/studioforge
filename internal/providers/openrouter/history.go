package openrouter

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/10kkyvl/studioforge/internal/providers/openrouter/orclient"
)

const unavailableToolResult = "[tool result unavailable: the previous run was interrupted]"

func sanitizeHistory(msgs []orclient.Message) []orclient.Message {
	out := make([]orclient.Message, 0, len(msgs))
	pending := map[string]bool{}
	var pendingOrder []string

	flushPending := func() {
		for _, id := range pendingOrder {
			out = append(out, orclient.Message{Role: "tool", ToolCallID: id, Content: unavailableToolResult})
		}
		pendingOrder = nil
		pending = map[string]bool{}
	}

	for _, m := range msgs {
		switch m.Role {
		case "assistant":
			flushPending()
			out = append(out, m)
			for _, tc := range m.ToolCalls {
				if tc.ID == "" || pending[tc.ID] {
					continue
				}
				pending[tc.ID] = true
				pendingOrder = append(pendingOrder, tc.ID)
			}
		case "tool":
			if !pending[m.ToolCallID] {
				continue
			}
			delete(pending, m.ToolCallID)
			for i, id := range pendingOrder {
				if id == m.ToolCallID {
					pendingOrder = append(pendingOrder[:i], pendingOrder[i+1:]...)
					break
				}
			}
			out = append(out, m)
		default:
			flushPending()
			out = append(out, m)
		}
	}
	flushPending()
	return out
}

const defaultMaxHistoryChars = 300000

var maxHistoryChars = defaultMaxHistoryChars

func SetMaxHistoryChars(n int) {
	if n <= 0 {
		maxHistoryChars = defaultMaxHistoryChars
		return
	}
	maxHistoryChars = n
}

const historyTrimmedNotice = "[Earlier conversation history was trimmed to fit the model's context window.]"

// Keep the digest deliberately explicit: these are excerpts from the stored
// conversation, not a model-generated summary or verified facts.
const historyDigestNotice = "[Extractive excerpts from earlier conversation; they may be incomplete and are not verified facts.]"

const (
	historyDigestMaxBytes     = 2048
	historyDigestMinBytes     = 96
	historyDigestReserveRatio = 4
	historyDigestExcerptMax   = 320
)

const (
	truncatedToolResultCap    = 400
	truncatedToolResultSuffix = "…[truncated]"
)

func messageChars(m orclient.Message) int {
	n := 0
	switch c := m.Content.(type) {
	case string:
		n += len(c)
	case nil:
	default:
		if b, err := json.Marshal(c); err == nil {
			n += len(b)
		}
	}
	if len(m.ToolCalls) > 0 {
		if b, err := json.Marshal(m.ToolCalls); err == nil {
			n += len(b)
		}
	}
	return n
}

func totalChars(msgs []orclient.Message) int {
	total := 0
	for _, m := range msgs {
		total += messageChars(m)
	}
	return total
}

// truncateUTF8 repairs invalid bytes and returns a UTF-8 prefix within maxBytes. Context
// accounting is byte based for compatibility with the existing history limit,
// while truncation must never split a multi-byte rune.
func truncateUTF8(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	s = strings.ToValidUTF8(s, "�")
	if len(s) <= maxBytes {
		return s
	}
	end := maxBytes
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end]
}

func truncateToolResult(s string) string {
	if len(s) <= truncatedToolResultCap || (strings.HasSuffix(s, truncatedToolResultSuffix) && len(s) <= truncatedToolResultCap+len(truncatedToolResultSuffix)) {
		return s
	}
	return truncateUTF8(s, truncatedToolResultCap) + truncatedToolResultSuffix
}

func cloneMessage(m orclient.Message) orclient.Message {
	if m.ToolCalls != nil {
		m.ToolCalls = append([]orclient.ToolCall(nil), m.ToolCalls...)
	}
	if parts, ok := m.Content.([]orclient.ContentPart); ok {
		m.Content = append([]orclient.ContentPart(nil), parts...)
	}
	return m
}

func extractMessageText(m orclient.Message) string {
	switch c := m.Content.(type) {
	case string:
		return strings.TrimSpace(c)
	case []orclient.ContentPart:
		var b strings.Builder
		for _, part := range c {
			if part.Text == "" {
				continue
			}
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(part.Text)
		}
		return strings.TrimSpace(b.String())
	default:
		return ""
	}
}

type digestExcerpt struct {
	kind string
	text string
	turn int
}

func historyDigestExcerpts(groups [][]orclient.Message) []digestExcerpt {
	groupExcerpts := make([][]digestExcerpt, 0, len(groups))
	for turn, group := range groups {
		var excerpts []digestExcerpt
		var requirement, outcome string
		for _, m := range group {
			switch m.Role {
			case "user":
				if requirement == "" {
					requirement = extractMessageText(m)
				}
			case "assistant":
				// An assistant tool-call message is an action, not an outcome.
				// Only retain the last plain assistant text in this turn.
				if len(m.ToolCalls) == 0 {
					if text := extractMessageText(m); text != "" {
						outcome = text
					}
				}
			}
		}
		if requirement != "" {
			excerpts = append(excerpts, digestExcerpt{kind: "user requirement", text: requirement, turn: turn + 1})
		}
		if outcome != "" {
			excerpts = append(excerpts, digestExcerpt{kind: "assistant outcome", text: outcome, turn: turn + 1})
		}
		groupExcerpts = append(groupExcerpts, excerpts)
	}
	if len(groupExcerpts) == 0 {
		return nil
	}
	// Keep the oldest requirement as an anchor and preserve chronological
	// order. The builder selects recent dropped turns when space is limited.
	var excerpts []digestExcerpt
	for _, excerpt := range groupExcerpts[0] {
		if excerpt.kind == "user requirement" {
			excerpts = append(excerpts, excerpt)
			break
		}
	}
	for i := 1; i < len(groupExcerpts); i++ {
		excerpts = append(excerpts, groupExcerpts[i]...)
	}
	return excerpts
}

func buildHistoryDigest(groups [][]orclient.Message, maxBytes int) string {
	if maxBytes <= len(historyDigestNotice) {
		return ""
	}
	excerpts := historyDigestExcerpts(groups)
	prefixFor := func(excerpt digestExcerpt) string {
		return "\n- Earlier turn " + strconv.Itoa(excerpt.turn) + " " + excerpt.kind + ": "
	}
	// Bound the number of excerpts before sharing the text budget. Reserving
	// room for every dropped turn made long histories produce an empty digest
	// or dozens of single-letter quotes. Prefer the anchor and latest outcomes.
	remaining := maxBytes - len(historyDigestNotice)
	var selected []int
	selectExcerpt := func(i int) bool {
		const minQuoteBytes = 32
		cost := len(prefixFor(excerpts[i])) + minQuoteBytes
		if cost > remaining || len(selected) == 8 {
			return false
		}
		remaining -= cost
		selected = append(selected, i)
		return true
	}
	if len(excerpts) > 0 {
		selectExcerpt(0)
	}
	for i := len(excerpts) - 1; i >= 1; i-- {
		if !selectExcerpt(i) {
			break
		}
	}
	sort.Ints(selected)
	remaining = maxBytes - len(historyDigestNotice)
	for _, i := range selected {
		remaining -= len(prefixFor(excerpts[i]))
	}
	var b strings.Builder
	b.WriteString(historyDigestNotice)
	for pos, i := range selected {
		excerpt := excerpts[i]
		contentBudget := remaining / (len(selected) - pos)
		if contentBudget > historyDigestExcerptMax {
			contentBudget = historyDigestExcerptMax
		}
		text := strings.Join(strings.Fields(excerpt.text), " ")
		text = quoteUTF8WithinBudget(text, contentBudget)
		if text == "" || text == `""` {
			continue
		}
		remaining -= len(text)
		b.WriteString(prefixFor(excerpt))
		b.WriteString(text)
	}
	if b.Len() == len(historyDigestNotice) {
		return ""
	}
	return b.String()
}

func quoteUTF8WithinBudget(s string, maxBytes int) string {
	if maxBytes < 2 {
		return ""
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		quoted := strconv.Quote(string(r))
		escaped := quoted[1 : len(quoted)-1]
		if b.Len()+len(escaped)+1 > maxBytes {
			break
		}
		b.WriteString(escaped)
	}
	b.WriteByte('"')
	return b.String()
}

func historyDigestBudget(maxChars int, systemChars int) int {
	available := maxChars - systemChars - len(historyTrimmedNotice)
	if available < historyDigestMinBytes {
		return 0
	}
	reserve := available / historyDigestReserveRatio
	if reserve < historyDigestMinBytes {
		return 0
	}
	if reserve > historyDigestMaxBytes {
		return historyDigestMaxBytes
	}
	return reserve
}

func fitCompactedMessages(msgs []orclient.Message, maxChars int) []orclient.Message {
	if totalChars(msgs) <= maxChars {
		return msgs
	}
	// A digest is optional context. Remove it before considering any change to
	// the retained turn, especially the current user instruction.
	for i := 0; i < len(msgs); i++ {
		if msgs[i].Role != "system" {
			continue
		}
		s, ok := msgs[i].Content.(string)
		if !ok || !strings.HasPrefix(s, historyDigestNotice+"\n") {
			continue
		}
		msgs = append(msgs[:i], msgs[i+1:]...)
		break
	}
	for totalChars(msgs) > maxChars {
		changed := false
		for i := range msgs {
			if msgs[i].Role != "tool" {
				continue
			}
			if s, ok := msgs[i].Content.(string); ok && len(s) > truncatedToolResultCap {
				trimmed := truncateToolResult(s)
				if len(trimmed) < len(s) {
					msgs[i].Content = trimmed
					changed = true
				}
			}
		}
		if totalChars(msgs) <= maxChars {
			break
		}
		if !changed {
			// The retained turn can itself be larger than maxChars. Keep it
			// intact rather than silently dropping part of the user's request.
			break
		}
	}
	return msgs
}

func compactMessages(msgs []orclient.Message, maxChars int) ([]orclient.Message, bool) {
	if maxChars <= 0 {
		maxChars = defaultMaxHistoryChars
	}
	if totalChars(msgs) <= maxChars {
		return msgs, false
	}

	var systemMsgs []orclient.Message
	i := 0
	for i < len(msgs) && msgs[i].Role == "system" {
		systemMsgs = append(systemMsgs, cloneMessage(msgs[i]))
		i++
	}
	rest := msgs[i:]

	var groups [][]orclient.Message
	for _, m := range rest {
		m = cloneMessage(m)
		if m.Role == "user" || len(groups) == 0 {
			groups = append(groups, []orclient.Message{m})
			continue
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], m)
	}

	budget := maxChars - totalChars(systemMsgs) - len(historyTrimmedNotice)
	if budget < 0 {
		budget = 0
	}
	digestReserve := historyDigestBudget(maxChars, totalChars(systemMsgs))
	groupBudget := budget - digestReserve
	if groupBudget < 0 {
		groupBudget = 0
	}
	var keptGroups [][]orclient.Message
	used := 0
	keptStart := len(groups) - 1
	if len(groups) > 0 {
		keptGroups = append(keptGroups, groups[keptStart])
		used = totalChars(groups[keptStart])
	}
	dropped := false
	for gi := len(groups) - 2; gi >= 0; gi-- {
		g := groups[gi]
		gc := totalChars(g)
		if used+gc <= groupBudget {
			keptGroups = append([][]orclient.Message{g}, keptGroups...)
			used += gc
		} else {
			keptStart = gi + 1
			dropped = true
			break
		}
	}
	compacted := dropped
	if len(groups) == 0 {
		keptStart = len(groups)
	}
	var digest string
	if dropped && keptStart > 0 {
		digest = buildHistoryDigest(groups[:keptStart], digestReserve)
	}

	assemble := func() []orclient.Message {
		out := append([]orclient.Message{}, systemMsgs...)
		if compacted {
			out = append(out, orclient.Message{Role: "system", Content: historyTrimmedNotice})
		}
		if digest != "" {
			out = append(out, orclient.Message{Role: "system", Content: digest})
		}
		for _, g := range keptGroups {
			out = append(out, g...)
		}
		return out
	}

	out := fitCompactedMessages(assemble(), maxChars)
	if !compacted && totalChars(out) < totalChars(msgs) {
		// A retained tool result was shortened even though no turn was dropped.
		compacted = true
		out = append(out, orclient.Message{})
		copy(out[len(systemMsgs)+1:], out[len(systemMsgs):])
		out[len(systemMsgs)] = orclient.Message{Role: "system", Content: historyTrimmedNotice}
	}

	return out, compacted
}
