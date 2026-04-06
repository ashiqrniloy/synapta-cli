package components

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/atotto/clipboard"

	"github.com/ashiqrniloy/synapta-cli/internal/core"
	"github.com/ashiqrniloy/synapta-cli/internal/core/tools"
	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

func shortHash(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:12]
}

func detectGitBranch(cwd string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return ""
	}
	cmd := exec.Command("git", "-C", cwd, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	branch := strings.TrimSpace(string(out))
	if branch == "" || branch == "HEAD" {
		return ""
	}
	return branch
}

func (m *CodeAgentModel) providerDisplayLabel() string {
	switch m.selectedProvider {
	case "kilo":
		return "Kilo Gateway"
	case "github-copilot":
		return "GitHub Copilot"
	case "":
		return "No provider"
	default:
		return m.selectedProvider
	}
}

func estimateMessagesTokens(messages []llm.Message) int {
	total := 0
	for _, msg := range messages {
		total += estimateTextTokens(msg.Content)
	}
	return total
}

func estimateTextTokens(text string) int {
	if strings.TrimSpace(text) == "" {
		return 0
	}
	return (len(text) + 3) / 4
}

func (m *CodeAgentModel) densityMode() string {
	if m.cfg != nil {
		d := strings.ToLower(strings.TrimSpace(m.cfg.UI.Density))
		if d == "compact" || d == "comfortable" {
			return d
		}
	}
	return "comfortable"
}

func (m *CodeAgentModel) isCompactDensity() bool { return m.densityMode() == "compact" }

func (m *CodeAgentModel) isStackedLayout() bool {
	return strings.TrimSpace(m.layoutMode) == layoutModeStacked
}

func (m *CodeAgentModel) toggleLayoutMode() {
	if m.isStackedLayout() {
		m.layoutMode = layoutModeSplit
		m.recordContextAction("Layout switched to split panes")
		return
	}
	m.layoutMode = layoutModeStacked
	m.recordContextAction("Layout switched to stacked panes")
}

func (m *CodeAgentModel) keybindingRows() []keybindingRow {
	newline := "shift+enter"
	if m.cfg != nil && m.cfg.Keybindings.Newline != "" {
		newline = normalizeKeyName(m.cfg.Keybindings.Newline)
	}
	return []keybindingRow{
		{Action: "Submit", Binding: m.getSubmitKey(), Description: "Send message / run bash"},
		{Action: "Newline", Binding: newline, Description: "Insert newline in input"},
		{Action: "Command palette", Binding: m.getCommandKey(), Description: "Open command picker"},
		{Action: "Extensions", Binding: m.extensionKeybinding(), Description: "Open extension launcher"},
		{Action: "Context manager", Binding: m.getContextKey(), Description: "Open context modal"},
		{Action: "Keybindings help", Binding: m.getHelpKey(), Description: "Open keybindings modal"},
		{Action: "Toggle tool expansion", Binding: "ctrl+o", Description: "Expand/collapse latest tool block"},
		{Action: "Copy latest block", Binding: "ctrl+shift+c / ctrl+y", Description: "Copy latest message/tool output to clipboard"},
		{Action: "Thinking level", Binding: "ctrl+t", Description: "Cycle thinking level for selected model"},
		{Action: "Skill picker", Binding: "@", Description: "Open skills suggestions"},
		{Action: "Quit", Binding: m.getQuitKey(), Description: "Exit Synapta Code"},
	}
}

func (m *CodeAgentModel) filteredKeybindingRows() []keybindingRow {
	rows := m.keybindingRows()
	q := strings.ToLower(strings.TrimSpace(m.keybindingsSearch))
	if q == "" {
		return rows
	}
	filtered := make([]keybindingRow, 0, len(rows))
	for _, r := range rows {
		h := strings.ToLower(r.Action + " " + r.Binding + " " + r.Description)
		if strings.Contains(h, q) {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

func (m *CodeAgentModel) recalculateLayout() {
	if m.width < 1 || m.height < 1 {
		return
	}
	m.ta.SetWidth(max(m.width-4, 20))
	inputH := countLines(m.renderInputBox())
	footerH := countLines(m.renderModelFooter())
	chatH := m.height - (2 + inputH + footerH)
	if chatH < 3 {
		chatH = 3
	}
	m.chatPaneWidth = m.width
	m.contextPaneWidth = 0
	m.contextPaneHeight = 0
	m.chatViewport.SetWidth(max(m.chatPaneWidth, 20))
	m.chatViewport.SetHeight(chatH)
}

func (m *CodeAgentModel) refreshChatViewport() {
	m.recalculateLayout()
	m.chatViewport.SetContent(m.renderChatTranscript())
	if m.chatAutoScroll {
		m.chatViewport.GotoBottom()
	}
}

func (m *CodeAgentModel) appendChatMessage(msg ChatMessage) int {
	insertAt := len(m.chatMessages)
	if m.activeSystemStatusIdx >= 0 && m.activeSystemStatusIdx < len(m.chatMessages) {
		insertAt = m.activeSystemStatusIdx
		m.chatMessages = append(m.chatMessages, ChatMessage{})
		copy(m.chatMessages[insertAt+1:], m.chatMessages[insertAt:])
		m.chatMessages[insertAt] = msg
		m.activeSystemStatusIdx++
		if m.activeAssistantIdx >= insertAt {
			m.activeAssistantIdx++
		}
		for id, idx := range m.activeToolIndices {
			if idx >= insertAt {
				m.activeToolIndices[id] = idx + 1
			}
		}
	} else {
		m.chatMessages = append(m.chatMessages, msg)
	}
	m.chatAutoScroll = true
	return insertAt
}

func (m *CodeAgentModel) appendSystemMessage(content, kind string) {
	m.appendChatMessage(ChatMessage{Role: "system", Content: content, SystemKind: kind})
	m.refreshChatViewport()
}

func (m *CodeAgentModel) setWorkingSystemMessage(content string) {
	if m.activeSystemStatusIdx >= 0 && m.activeSystemStatusIdx < len(m.chatMessages) {
		m.chatMessages[m.activeSystemStatusIdx].Content = content
		m.chatMessages[m.activeSystemStatusIdx].SystemKind = "working"
		return
	}
	m.activeSystemStatusIdx = len(m.chatMessages)
	m.chatMessages = append(m.chatMessages, ChatMessage{Role: "system", Content: content, SystemKind: "working"})
	m.chatAutoScroll = true
}

func (m *CodeAgentModel) updateWorkingSystemMessage(content string) {
	if m.activeSystemStatusIdx >= 0 && m.activeSystemStatusIdx < len(m.chatMessages) {
		m.chatMessages[m.activeSystemStatusIdx].Content = content
		m.chatMessages[m.activeSystemStatusIdx].SystemKind = "working"
	}
}

func (m *CodeAgentModel) finalizeWorkingSystemMessage(content, kind string) {
	if m.activeSystemStatusIdx >= 0 && m.activeSystemStatusIdx < len(m.chatMessages) {
		m.chatMessages[m.activeSystemStatusIdx].Content = content
		m.chatMessages[m.activeSystemStatusIdx].SystemKind = kind
		m.activeSystemStatusIdx = -1
		m.chatAutoScroll = true
		return
	}
	m.appendSystemMessage(content, kind)
}

func (m *CodeAgentModel) chatWorkingStatusText() string {
	spinner := []string{"⠋", "⠙", "⠹", "⠸"}[m.workingFrame%4]
	elapsed := time.Since(m.streamStartedAt).Round(time.Second)
	if m.streamStartedAt.IsZero() {
		elapsed = 0
	}
	return fmt.Sprintf("%s Working with %s/%s... %s", spinner, m.selectedProvider, m.selectedModelID, elapsed)
}

func (m *CodeAgentModel) bashWorkingStatusText() string {
	spinner := []string{"⠋", "⠙", "⠹", "⠸"}[m.workingFrame%4]
	return fmt.Sprintf("[Bash] %s Running command...", spinner)
}

func formatToolContextContent(e core.ToolEvent) string {
	var b strings.Builder
	b.WriteString("Tool: ")
	b.WriteString(strings.TrimSpace(e.ToolName))
	b.WriteString("\n")
	if strings.TrimSpace(e.Path) != "" {
		b.WriteString("Path: ")
		b.WriteString(strings.TrimSpace(e.Path))
		b.WriteString("\n")
	}
	if strings.TrimSpace(e.Command) != "" {
		b.WriteString("Command: ")
		b.WriteString(strings.TrimSpace(e.Command))
		b.WriteString("\n")
	}
	b.WriteString("State: ")
	if e.IsError {
		b.WriteString("error\n")
	} else {
		b.WriteString("done\n")
	}
	b.WriteString("\nOutput:\n")
	if strings.TrimSpace(e.Output) == "" {
		b.WriteString("(no output)")
	} else {
		b.WriteString(strings.TrimSpace(e.Output))
	}
	return b.String()
}

func (m *CodeAgentModel) chatHistoryAsLLM() ([]llm.Message, error) {
	baseHistory := m.conversationHistory
	if m.sessionStore != nil && !m.contextOverrideActive {
		contextWindow := 128000
		if m.chatService != nil && m.selectedProvider != "" && m.selectedModelID != "" {
			if cw, err := m.chatService.ModelContextWindow(context.Background(), m.selectedProvider, m.selectedModelID); err == nil && cw > 0 {
				contextWindow = cw
			}
		}
		summarizer := func(ctx context.Context, toSummarize []llm.Message, previousSummary string) (string, error) {
			if m.chatService == nil || m.selectedProvider == "" || m.selectedModelID == "" {
				return "", nil
			}
			return m.chatService.SummarizeCompaction(ctx, m.selectedProvider, m.selectedModelID, toSummarize, previousSummary)
		}
		compacted, err := m.sessionStore.CompactIfNeeded(context.Background(), contextWindow, summarizer)
		if err != nil {
			return nil, err
		}
		if compacted {
			m.recordContextAction("Auto compaction applied")
		}
		baseHistory = m.sessionStore.ContextMessages()
		m.conversationHistory = append([]llm.Message(nil), baseHistory...)
	}
	if m.contextManager == nil {
		return append([]llm.Message(nil), baseHistory...), nil
	}
	msgs, err := m.contextManager.Build(baseHistory)
	if err != nil {
		return nil, err
	}
	fp := m.contextManager.LastPromptFingerprint()
	if fp.PromptHash != "" {
		m.promptBuildCount++
		m.likelyPromptCacheHit = m.lastPromptFingerprint.StablePrefixHash != "" && fp.StablePrefixHash == m.lastPromptFingerprint.StablePrefixHash
		if m.lastPromptFingerprint.StablePrefixHash != "" && fp.StablePrefixHash != m.lastPromptFingerprint.StablePrefixHash {
			m.stablePrefixChangeCount++
		}
		if fp.PromptHash != m.lastPromptHash {
			m.lastPromptHash = fp.PromptHash
			m.recordContextAction(fmt.Sprintf("Prompt fingerprint updated: %s", fp.PromptHash[:12]))
		}
		m.lastPromptFingerprint = fp
	}
	if instruction := thinkingInstruction(m.selectedThinkingLevel); instruction != "" {
		sys := llm.Message{Role: "system", Content: instruction}
		insertAt := 0
		for insertAt < len(msgs) && msgs[insertAt].Role == "system" {
			insertAt++
		}
		withThinking := make([]llm.Message, 0, len(msgs)+1)
		withThinking = append(withThinking, msgs[:insertAt]...)
		withThinking = append(withThinking, sys)
		withThinking = append(withThinking, msgs[insertAt:]...)
		msgs = withThinking
	}
	return msgs, nil
}

func (m *CodeAgentModel) hasRunningTool() bool {
	for _, msg := range m.chatMessages {
		if msg.Role == "tool" && msg.ToolState == "running" {
			return true
		}
	}
	return false
}

func (m *CodeAgentModel) lastToolCallID() (string, bool) {
	for i := len(m.chatMessages) - 1; i >= 0; i-- {
		msg := m.chatMessages[i]
		if msg.Role == "tool" && msg.ToolCallID != "" {
			return msg.ToolCallID, true
		}
	}
	return "", false
}

func (m *CodeAgentModel) copyLatestMessageToClipboard() error {
	text := strings.TrimSpace(m.latestTranscriptBlockForCopy())
	if text == "" {
		return fmt.Errorf("nothing to copy")
	}
	if err := clipboard.WriteAll(text); err != nil {
		return fmt.Errorf("clipboard unavailable: %w", err)
	}
	return nil
}

func (m *CodeAgentModel) latestTranscriptBlockForCopy() string {
	for i := len(m.chatMessages) - 1; i >= 0; i-- {
		msg := m.chatMessages[i]
		switch msg.Role {
		case "tool":
			var b strings.Builder
			b.WriteString("Tool: ")
			b.WriteString(strings.TrimSpace(msg.ToolName))
			b.WriteString("\n")
			if p := strings.TrimSpace(msg.ToolPath); p != "" {
				b.WriteString("Path: ")
				b.WriteString(p)
				b.WriteString("\n")
			}
			if c := strings.TrimSpace(msg.ToolCommand); c != "" {
				b.WriteString("Command: ")
				b.WriteString(c)
				b.WriteString("\n")
			}
			state := strings.TrimSpace(msg.ToolState)
			if state == "" {
				state = "running"
			}
			b.WriteString("State: ")
			b.WriteString(state)
			b.WriteString("\n\n")
			if out := strings.TrimSpace(msg.Content); out != "" {
				b.WriteString(out)
			} else {
				b.WriteString("(no output)")
			}
			return b.String()
		case "assistant", "user", "system":
			if t := strings.TrimSpace(msg.Content); t != "" {
				return t
			}
		}
	}
	return ""
}

func (m *CodeAgentModel) toolPreviewLines() int {
	h := m.chatViewport.Height()
	if h <= 0 {
		return 10
	}
	lines := h / 3
	if lines < 5 {
		lines = 5
	}
	if lines > 20 {
		lines = 20
	}
	return lines
}

func parseCDCommand(command string) (target string, ok bool) {
	trimmed := strings.TrimSpace(command)
	if trimmed == "cd" {
		return "~", true
	}
	if !strings.HasPrefix(trimmed, "cd ") {
		return "", false
	}
	target = strings.TrimSpace(strings.TrimPrefix(trimmed, "cd"))
	if target == "" {
		return "~", true
	}
	if strings.Contains(target, "&&") || strings.Contains(target, "||") || strings.ContainsAny(target, ";|><`") {
		return "", false
	}
	if (strings.HasPrefix(target, "\"") && strings.HasSuffix(target, "\"")) || (strings.HasPrefix(target, "'") && strings.HasSuffix(target, "'")) {
		target = target[1 : len(target)-1]
	}
	return target, true
}

func resolveCDTarget(baseCwd, target string) (string, error) {
	if strings.HasPrefix(target, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if target == "~" {
			target = home
		} else {
			target = filepath.Join(home, strings.TrimPrefix(target, "~/"))
		}
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(baseCwd, target)
	}
	resolved, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", resolved)
	}
	return resolved, nil
}

func toolResultPlainText(result tools.Result) string {
	var b strings.Builder
	for _, c := range result.Content {
		if c.Type == tools.ContentPartText && c.Text != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(c.Text)
		}
	}
	return strings.TrimSpace(b.String())
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (m *CodeAgentModel) rebuildTranscriptFromHistory() {
	messages := make([]ChatMessage, 0, len(m.conversationHistory))
	for _, msg := range m.conversationHistory {
		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		messages = append(messages, ChatMessage{Role: msg.Role, Content: content})
	}
	m.chatMessages = messages
	m.activeAssistantIdx = -1
	m.activeToolIndices = map[string]int{}
	m.refreshChatViewport()
}
