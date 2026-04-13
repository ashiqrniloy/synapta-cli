package components

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

func (m *CodeAgentModel) renderHeaderBar() string {
	title := m.styles.TitleStyle.Render("SYNAPTA CODE")
	return lipgloss.NewStyle().
		Width(m.width).
		Align(lipgloss.Center).
		Render(truncateLine(title, m.width))
}

func (m *CodeAgentModel) renderInputBox() string {
	taView := strings.TrimRight(m.ta.View(), "\n")
	innerWidth := max(m.width-4, 20)
	if m.picker.IsActive() {
		taView = lipgloss.NewStyle().Foreground(m.styles.MutedStyle.GetForeground()).Render(taView)
		pickerView := strings.TrimRight(m.picker.View(innerWidth), "\n")
		if pickerView != "" {
			divider := lipgloss.NewStyle().Foreground(m.styles.MutedStyle.GetForeground()).Render(strings.Repeat("─", max(innerWidth, 1)))
			taView += "\n" + divider + "\n" + pickerView
		}
	} else if m.skillPicker != nil && m.skillPicker.IsActive() {
		taView = lipgloss.NewStyle().Foreground(m.styles.MutedStyle.GetForeground()).Render(taView)
		pickerView := strings.TrimRight(m.skillPicker.View(innerWidth), "\n")
		if pickerView != "" {
			divider := lipgloss.NewStyle().Foreground(m.styles.MutedStyle.GetForeground()).Render(strings.Repeat("─", max(innerWidth, 1)))
			taView += "\n" + divider + "\n" + pickerView
		}
	} else if m.fileBrowser != nil && m.fileBrowser.IsActive() {
		taView = lipgloss.NewStyle().Foreground(m.styles.MutedStyle.GetForeground()).Render(taView)
		browserView := strings.TrimRight(m.fileBrowser.View(innerWidth), "\n")
		if browserView != "" {
			divider := lipgloss.NewStyle().Foreground(m.styles.MutedStyle.GetForeground()).Render(strings.Repeat("─", max(innerWidth, 1)))
			taView += "\n" + divider + "\n" + browserView
		}
	} else if m.sessionSearch != nil && m.sessionSearch.IsActive() {
		// Session search is active - show results below the textarea
		// The textarea already shows "> query" text
		taView = lipgloss.NewStyle().Foreground(m.styles.MutedStyle.GetForeground()).Render(taView)
		searchView := strings.TrimRight(m.sessionSearch.View(innerWidth), "\n")
		if searchView != "" {
			divider := lipgloss.NewStyle().Foreground(m.styles.MutedStyle.GetForeground()).Render(strings.Repeat("─", max(innerWidth, 1)))
			taView += "\n" + divider + "\n" + searchView
		}
	}
	borderStyle := lipgloss.NewStyle().
		Width(m.width).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(m.borderColor)).
		Padding(0, 1)
	return borderStyle.Render(taView)
}

func (m *CodeAgentModel) renderModelFooter() string {
	muted := lipgloss.NewStyle().Foreground(m.styles.MutedStyle.GetForeground())

	thinking := m.selectedThinkingLevel
	if thinking == "" {
		thinking = inferThinkingLevel(m.selectedModelID, m.selectedModelName)
	}
	window := m.selectedContextWindow
	if window <= 0 {
		window = resolveModelContextWindow(m.selectedProvider, m.selectedModelID)
	}
	usedTokens := estimateMessagesTokens(m.conversationHistory)
	contextText := fmt.Sprintf("context used: ~%s", formatTokenCount(usedTokens))
	if window > 0 {
		pct := int((float64(usedTokens) / float64(window)) * 100)
		if pct < 0 {
			pct = 0
		}
		contextText = fmt.Sprintf("context used: ~%s / %s (%d%%)", formatTokenCount(usedTokens), formatTokenCount(window), pct)
	}

	llmText := "LLM: (none)"
	if strings.TrimSpace(m.selectedModelName) != "" {
		llmText = fmt.Sprintf("LLM: %s  •  thinking: %s (Ctrl+T)", m.selectedModelName, thinking)
	}

	provider := m.providerDisplayLabel()
	if m.providerBalance != "" && (m.selectedProvider == "kilo" || m.selectedProvider == "github-copilot") {
		provider += " · " + m.providerBalance
	}
	rightBottom := contextText
	if strings.TrimSpace(provider) != "" {
		rightBottom = contextText + "  •  provider: " + provider
	}

	branchLine := "branch: (none)"
	if strings.TrimSpace(m.currentGitBranch) != "" {
		branchLine = "branch: " + m.currentGitBranch
	}
	cwdLine := "cwd: " + m.currentCwd

	leftW := max((m.width*55)/100, 1)
	if leftW >= m.width {
		leftW = max(m.width-1, 1)
	}
	rightW := max(m.width-leftW, 1)

	line1 := lipgloss.JoinHorizontal(
		lipgloss.Top,
		lipgloss.NewStyle().Width(leftW).Align(lipgloss.Left).Render(truncateLine(branchLine, leftW)),
		lipgloss.NewStyle().Width(rightW).Align(lipgloss.Right).Render(truncateLine(llmText, rightW)),
	)
	line2 := lipgloss.JoinHorizontal(
		lipgloss.Top,
		lipgloss.NewStyle().Width(leftW).Align(lipgloss.Left).Render(truncateLine(cwdLine, leftW)),
		lipgloss.NewStyle().Width(rightW).Align(lipgloss.Right).Render(truncateLine(rightBottom, rightW)),
	)
	return muted.Render(line1 + "\n" + line2)
}

func (m *CodeAgentModel) renderContextPane(height int) string {
	width := max(m.contextPaneWidth, 20)
	innerWidth := max(width-4, 10)
	innerHeight := max(height-2, 8)

	muted := lipgloss.NewStyle().Foreground(m.styles.MutedStyle.GetForeground())
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(m.styles.CommandHighlightStyle.GetForeground()).
		Width(innerWidth).
		Align(lipgloss.Center).
		Render("CONTEXT")

	if m.isStackedLayout() {
		entries := m.buildContextEntries()

		actionItems := make([]string, 0, len(m.contextActions))
		for i := len(m.contextActions) - 1; i >= 0; i-- {
			a := m.contextActions[i]
			actionItems = append(actionItems, truncateLine(a.Message, 30))
		}

		skillItems := make([]string, 0, len(m.availableSkills))
		for _, s := range m.availableSkills {
			skillItems = append(skillItems, truncateLine("@"+s.Name, 30))
		}

		contextItems := make([]string, 0, len(entries))
		usedTokens := estimateMessagesTokens(m.conversationHistory)
		maxTokens := m.selectedContextWindow
		if maxTokens <= 0 {
			maxTokens = resolveModelContextWindow(m.selectedProvider, m.selectedModelID)
		}
		usage := fmt.Sprintf("Used ~%s", formatTokenCount(usedTokens))
		if maxTokens > 0 {
			pct := int((float64(usedTokens) / float64(maxTokens)) * 100)
			if pct < 0 {
				pct = 0
			}
			usage = fmt.Sprintf("Used ~%s / %s (%d%%)", formatTokenCount(usedTokens), formatTokenCount(maxTokens), pct)
		}
		for _, e := range entries {
			label := strings.TrimSpace(e.Label)
			if label == "" {
				label = e.Role
			}
			contextItems = append(contextItems, truncateLine(label, 30))
		}

		colW := max((innerWidth-2)/3, 30)
		rows := max(innerHeight-4, 5)
		maxRows := max(max(len(actionItems), len(skillItems)), len(contextItems))
		if maxRows > 0 && maxRows < rows {
			rows = maxRows
		}
		if rows < 5 {
			rows = 5
		}

		pad := func(items []string) []string {
			out := make([]string, rows)
			for i := 0; i < rows; i++ {
				if i < len(items) {
					out[i] = truncateLine(items[i], colW)
				}
			}
			return out
		}

		left := pad(actionItems)
		mid := pad(skillItems)
		right := pad(contextItems)

		lines := []string{title, ""}
		currentContextHeader := truncateLine("CURRENT CONTEXT ("+usage+")", colW)
		headers := lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Width(colW).Align(lipgloss.Left).Render(muted.Render("ACTIONS")), " ",
			lipgloss.NewStyle().Width(colW).Align(lipgloss.Left).Render(muted.Render("KNOWN SKILLS")), " ",
			lipgloss.NewStyle().Width(colW).Align(lipgloss.Left).Render(muted.Render(currentContextHeader)),
		)
		lines = append(lines, headers)
		for i := 0; i < rows; i++ {
			line := lipgloss.JoinHorizontal(lipgloss.Top,
				lipgloss.NewStyle().Width(colW).Render(left[i]), " ",
				lipgloss.NewStyle().Width(colW).Render(mid[i]), " ",
				lipgloss.NewStyle().Width(colW).Render(right[i]),
			)
			lines = append(lines, line)
		}

		content := strings.Join(limitLines(lines, innerHeight), "\n")
		return lipgloss.NewStyle().
			Width(width).
			Height(max(height-2, 1)).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(m.borderColor)).
			Padding(0, 1).
			Render(content)
	}

	actionsRatio := 24
	skillsRatio := 16
	if m.isCompactDensity() {
		actionsRatio = 20
		skillsRatio = 12
	}
	actionsH := max((innerHeight*actionsRatio)/100, 3)
	skillsH := max((innerHeight*skillsRatio)/100, 3)
	contextH := max(innerHeight-actionsH-skillsH-6, 4)

	actionLines := make([]string, 0, actionsH)
	for i := len(m.contextActions) - 1; i >= 0 && len(actionLines) < actionsH; i-- {
		a := m.contextActions[i]
		line := fmt.Sprintf("%s  %s", a.At.Format("15:04:05"), a.Message)
		actionLines = append([]string{truncateLine(line, innerWidth)}, actionLines...)
	}
	for len(actionLines) < actionsH {
		actionLines = append(actionLines, "")
	}

	skillLines := make([]string, 0, skillsH)
	for _, s := range m.availableSkills {
		skillLines = append(skillLines, truncateLine("@"+s.Name, innerWidth))
		if len(skillLines) >= skillsH {
			break
		}
	}
	for len(skillLines) < skillsH {
		skillLines = append(skillLines, "")
	}

	entries := m.buildContextEntries()
	contextLines := make([]string, 0, contextH)
	for _, e := range entries {
		contextLines = append(contextLines, m.renderContextPreviewLine(e, innerWidth))
		if len(contextLines) >= contextH {
			break
		}
	}
	for len(contextLines) < contextH {
		contextLines = append(contextLines, "")
	}

	sep := muted.Render(strings.Repeat("─", max(innerWidth, 1)))
	lines := []string{title, ""}
	lines = append(lines, muted.Render("ACTIONS"))
	lines = append(lines, actionLines...)
	lines = append(lines, sep)
	lines = append(lines, muted.Render("KNOWN SKILLS"))
	lines = append(lines, skillLines...)
	lines = append(lines, sep)
	lines = append(lines, muted.Render("CURRENT CONTEXT"))
	usedTokens := estimateMessagesTokens(m.conversationHistory)
	maxTokens := m.selectedContextWindow
	if maxTokens <= 0 {
		maxTokens = resolveModelContextWindow(m.selectedProvider, m.selectedModelID)
	}
	if maxTokens > 0 {
		pct := int((float64(usedTokens) / float64(maxTokens)) * 100)
		if pct < 0 {
			pct = 0
		}
		lines = append(lines, muted.Render(truncateLine(fmt.Sprintf("Used ~%s / %s (%d%%)", formatTokenCount(usedTokens), formatTokenCount(maxTokens), pct), innerWidth)))
	} else {
		lines = append(lines, muted.Render(truncateLine(fmt.Sprintf("Used ~%s", formatTokenCount(usedTokens)), innerWidth)))
	}
	lines = append(lines, contextLines...)

	content := strings.Join(lines, "\n")
	return lipgloss.NewStyle().
		Width(width).
		Height(max(height-2, 1)).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(m.borderColor)).
		Padding(0, 1).
		Render(content)
}

func (m *CodeAgentModel) renderContextPreviewLine(e ContextEntry, width int) string {
	badge := renderContextBadge(e.Category)
	usage := fmt.Sprintf("~%s", formatTokenCount(e.EstimatedTokens))
	maxTokens := m.selectedContextWindow
	if maxTokens <= 0 {
		maxTokens = resolveModelContextWindow(m.selectedProvider, m.selectedModelID)
	}
	if maxTokens > 0 {
		pct := int((float64(e.EstimatedTokens) / float64(maxTokens)) * 100)
		if pct < 0 {
			pct = 0
		}
		usage = fmt.Sprintf("~%s (%d%%)", formatTokenCount(e.EstimatedTokens), pct)
	}
	line := fmt.Sprintf("%2d %s %s · %s", e.Order, badge, e.Label, usage)
	return truncateLine(line, width)
}

func (m *CodeAgentModel) renderContextDiagnostics(width int, height int) []string {
	fp := m.lastPromptFingerprint
	lines := []string{}
	if fp.PromptHash == "" {
		lines = append(lines, "Prompt: (not built yet)")
	} else {
		cacheState := "miss"
		if m.promptBuildCount <= 1 {
			cacheState = "cold-start"
		} else if m.likelyPromptCacheHit {
			cacheState = "hit-candidate"
		}
		lines = append(lines, truncateLine("Prompt hash: "+shortHash(fp.PromptHash), width))
		lines = append(lines, truncateLine("Stable: "+shortHash(fp.StablePrefixHash), width))
		lines = append(lines, truncateLine("History: "+shortHash(fp.HistoryHash), width))
		lines = append(lines, truncateLine(fmt.Sprintf("Cache: %s  builds:%d  stableΔ:%d", cacheState, m.promptBuildCount, m.stablePrefixChangeCount), width))
		lines = append(lines, truncateLine(fmt.Sprintf("Messages: %d", fp.MessageCount), width))
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return lines
}

func (m *CodeAgentModel) renderContextModal() string {
	width := m.width
	height := m.height

	if m.contextModalEntries == nil {
		m.contextModalEntries = m.buildContextEntries()
	}

	var body string
	if m.contextModalEditMode {
		m.contextModalEditor.SetWidth(max(width-8, 40))
		m.contextModalEditor.SetHeight(max(height-8, 8))
		body = m.contextModalEditor.View()
	} else {
		if m.contextModalSelection < 0 {
			m.contextModalSelection = 0
		}
		if m.contextModalSelection >= len(m.contextModalEntries) && len(m.contextModalEntries) > 0 {
			m.contextModalSelection = len(m.contextModalEntries) - 1
		}

		leftW := max((width-10)*25/100, 30)
		rightW := max((width-10)-leftW, 30)
		innerH := max(height-8, 8)

		listLines := []string{lipgloss.NewStyle().Bold(true).Render("Context Entries")}
		for i, e := range m.contextModalEntries {
			prefix := "  "
			if i == m.contextModalSelection {
				prefix = "▸ "
			}
			line := m.renderContextPreviewLine(e, leftW-4)
			listLines = append(listLines, truncateLine(prefix+line, leftW-2))
		}
		listContent := strings.Join(limitLines(listLines, innerH), "\n")
		leftPane := lipgloss.NewStyle().
			Width(leftW).
			Height(innerH).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(m.borderColor)).
			Padding(0, 1).
			Render(listContent)

		diag := m.renderContextDiagnostics(rightW-4, 5)
		previewLines := []string{lipgloss.NewStyle().Bold(true).Render("Diagnostics")}
		previewLines = append(previewLines, diag...)
		previewLines = append(previewLines, "", lipgloss.NewStyle().Bold(true).Render("Selected Entry"))
		selected := m.contextModalSelectedEntry()
		if selected != nil {
			usage := fmt.Sprintf("~%s", formatTokenCount(selected.EstimatedTokens))
			maxTokens := m.selectedContextWindow
			if maxTokens <= 0 {
				maxTokens = resolveModelContextWindow(m.selectedProvider, m.selectedModelID)
			}
			if maxTokens > 0 {
				pct := int((float64(selected.EstimatedTokens) / float64(maxTokens)) * 100)
				if pct < 0 {
					pct = 0
				}
				usage = fmt.Sprintf("~%s (%d%%)", formatTokenCount(selected.EstimatedTokens), pct)
			}
			previewLines = append(previewLines,
				fmt.Sprintf("#%d  %s", selected.Order, renderContextBadge(selected.Category)),
				lipgloss.NewStyle().Foreground(m.styles.MutedStyle.GetForeground()).Render("Role: "+selected.Role),
				lipgloss.NewStyle().Foreground(m.styles.MutedStyle.GetForeground()).Render("Estimated usage: "+usage),
			)

			contentLines := wrapMultiline(strings.TrimSpace(selected.Content), max(rightW-4, 20))
			if len(contentLines) == 0 {
				contentLines = []string{""}
			}
			contentViewportH := max(innerH-len(previewLines)-1, 1)
			maxOffset := max(len(contentLines)-contentViewportH, 0)
			if m.contextModalPreviewOffset > maxOffset {
				m.contextModalPreviewOffset = maxOffset
			}
			start := m.contextModalPreviewOffset
			end := min(start+contentViewportH, len(contentLines))
			previewLines = append(previewLines, lipgloss.NewStyle().Foreground(m.styles.MutedStyle.GetForeground()).Render(fmt.Sprintf("Scroll: j/k or wheel  (%d/%d)", start+1, maxOffset+1)))

			previewLines = append(previewLines, contentLines[start:end]...)
		} else {
			m.contextModalPreviewOffset = 0
			previewLines = append(previewLines, lipgloss.NewStyle().Foreground(m.styles.MutedStyle.GetForeground()).Render("No context entries"))
		}
		for len(previewLines) < innerH {
			previewLines = append(previewLines, "")
		}
		if len(previewLines) > innerH {
			previewLines = previewLines[:innerH]
		}
		previewContent := strings.Join(previewLines, "\n")
		rightPane := lipgloss.NewStyle().
			Width(rightW).
			Height(innerH).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(m.styles.CommandHighlightStyle.GetForeground()).
			Padding(0, 1).
			Render(previewContent)
		head := lipgloss.NewStyle().
			Width(max(width-6, 20)).
			Align(lipgloss.Center).
			Bold(true).
			Foreground(m.styles.CommandHighlightStyle.GetForeground()).
			Render("Context Manager")
		foot := "↑↓ select entry  •  j/k or wheel scroll preview  •  Enter/E edit  •  D remove  •  C compact  •  Esc close"

		body = head + "\n\n" + lipgloss.JoinHorizontal(lipgloss.Top, leftPane, " ", rightPane) + "\n\n" + foot
	}
	if m.contextModalEditorHint != "" {
		body += "\n\n" + m.contextModalEditorHint
	}
	modal := lipgloss.NewStyle().
		Width(width).
		Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.styles.CommandHighlightStyle.GetForeground()).
		Padding(1, 2).
		Render(body)
	return modal
}

func contextBadgeLabel(category string) (string, string) {
	bg := "240"
	label := category
	switch category {
	case "system-prompt":
		bg, label = "61", "System"
	case "skills":
		bg, label = "99", "Skill"
	case "compacted-output":
		bg, label = "172", "Compacted"
	case "files-read":
		bg, label = "31", "Read"
	case "files-written":
		bg, label = "29", "Write"
	case "tool-bash":
		bg, label = "130", "Bash"
	case "llm-output":
		bg, label = "64", "LLM"
	case "user-input":
		bg, label = "95", "User"
	case "tool-output":
		bg, label = "67", "Tool"
	}
	return bg, label
}

func (m *CodeAgentModel) renderCommandModal(base string) string {
	_ = base
	width := max(m.width-8, 60)
	height := max(m.height-8, 12)
	innerWidth := max(width-4, 20)

	inputView := strings.TrimRight(m.commandModalInput.View(), "\n")
	inputView = lipgloss.NewStyle().Foreground(m.styles.MutedStyle.GetForeground()).Render(inputView)
	pickerView := strings.TrimRight(m.picker.View(innerWidth), "\n")
	if pickerView != "" {
		divider := lipgloss.NewStyle().Foreground(m.styles.MutedStyle.GetForeground()).Render(strings.Repeat("─", max(innerWidth, 1)))
		inputView += "\n" + divider + "\n" + pickerView
	}

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(m.styles.CommandHighlightStyle.GetForeground()).
		Align(lipgloss.Center).
		Width(innerWidth).
		Render("COMMAND")

	body := title + "\n\n" + inputView
	modal := lipgloss.NewStyle().
		Width(width).
		Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.styles.CommandHighlightStyle.GetForeground()).
		Padding(1, 2).
		Render(body)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
}

func (m *CodeAgentModel) renderKeybindingsModal() string {
	rows := m.filteredKeybindingRows()
	if m.keybindingsSelection >= len(rows) {
		m.keybindingsSelection = max(len(rows)-1, 0)
	}

	width := max(m.width-8, 60)
	height := max(m.height-6, 16)
	listH := max(height-8, 8)

	lines := []string{lipgloss.NewStyle().Bold(true).Render("Keybindings")}
	search := m.keybindingsSearch
	if search == "" {
		search = "type to search..."
	}
	lines = append(lines, m.styles.MutedStyle.Render("Search: "+search), "")

	start := 0
	if m.keybindingsSelection >= listH {
		start = m.keybindingsSelection - listH + 1
	}
	end := min(start+listH, len(rows))
	if len(rows) == 0 {
		lines = append(lines, m.styles.MutedStyle.Render("No keybindings match search."))
	} else {
		for i := start; i < end; i++ {
			r := rows[i]
			line := fmt.Sprintf("%-18s  %-10s  %s", r.Action, r.Binding, r.Description)
			if i == m.keybindingsSelection {
				lines = append(lines, m.styles.CommandHighlightStyle.Render(truncateLine("▸ "+line, width-6)))
			} else {
				lines = append(lines, truncateLine("  "+line, width-6))
			}
		}
	}
	lines = append(lines, "", m.styles.MutedStyle.Render("↑↓ navigate  •  PgUp/PgDn scroll  •  type to filter  •  Backspace delete  •  Esc close"))

	body := strings.Join(lines, "\n")
	return lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		AlignHorizontal(lipgloss.Center).
		AlignVertical(lipgloss.Center).
		Render(lipgloss.NewStyle().
			Width(width).
			Height(height).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(m.styles.CommandHighlightStyle.GetForeground()).
			Padding(1, 2).
			Render(body))
}

func renderContextBadge(category string) string {
	bg, label := contextBadgeLabel(category)
	return lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Background(lipgloss.Color(bg)).Bold(true).Padding(0, 1).Render(label)
}

func (m *CodeAgentModel) renderChatTranscript() string {
	if len(m.chatMessages) == 0 {
		return ""
	}
	lines := make([]string, 0, len(m.chatMessages))
	sep := "\n\n"
	if m.isCompactDensity() {
		sep = "\n"
	}

	// Highlight when search is active OR when a match was selected (highlightLine >= 0)
	shouldHighlight := m.sessionSearch != nil && m.sessionSearch.IsActive()
	shouldHighlight = shouldHighlight || m.sessionSearchHighlightLine >= 0
	if shouldHighlight {
		m.chatRenderedLines = nil
		m.chatMessageStartLines = nil
	}

	highlightLine := m.sessionSearchHighlightLine
	var currentLine int

	for msgIdx, msg := range m.chatMessages {
		if shouldHighlight {
			m.chatMessageStartLines = append(m.chatMessageStartLines, currentLine)
		}

		var rendered string
		switch msg.Role {
		case "user":
			rendered = m.renderUserMessage(msg.Content)
		case "assistant":
			rendered = m.renderAssistantMessage(msg.Content)
		case "tool":
			rendered = m.renderToolMessage(msg)
		case "system":
			rendered = m.renderSystemMessage(msg)
		default:
			rendered = msg.Content
		}

		// Split rendered content into lines
		msgLines := strings.Split(rendered, "\n")
		for _, line := range msgLines {
			if shouldHighlight {
				// Track rendered lines for search
				m.chatRenderedLines = append(m.chatRenderedLines, line)
				// Apply highlight if this is the highlighted line
				if currentLine == highlightLine {
					line = applySearchHighlight(line)
				}
				currentLine++
			}
			lines = append(lines, line)
		}

		// Add separator between messages (except after last message)
		if msgIdx < len(m.chatMessages)-1 {
			sepLines := strings.Split(sep, "\n")
			for _, sepLine := range sepLines {
				if shouldHighlight {
					m.chatRenderedLines = append(m.chatRenderedLines, sepLine)
					if currentLine == highlightLine {
						sepLine = applySearchHighlight(sepLine)
					}
					currentLine++
				}
				lines = append(lines, sepLine)
			}
		}
	}

	return strings.Join(lines, sep)
}

// applySearchHighlight applies a visual highlight to a line.
func applySearchHighlight(line string) string {
	// Use a distinct style to highlight the search result line
	highlightStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("238")).
		Foreground(lipgloss.Color("255")).
		Bold(true)
	// Render the line with the highlight style
	return highlightStyle.Render(line)
}

func (m *CodeAgentModel) renderUserMessage(content string) string {
	maxWidth := max(m.chatViewport.Width(), 20)
	lines := wordWrap(content, maxWidth-2)
	if len(lines) == 0 {
		lines = []string{""}
	}
	padY := 0
	if !m.isCompactDensity() {
		padY = 1
	}
	return m.styles.InteractionHighlightStyle.
		Width(maxWidth).
		Padding(padY, 1).
		Render(strings.Join(lines, "\n"))
}

func (m *CodeAgentModel) renderAssistantMessage(content string) string {
	maxWidth := max(m.chatViewport.Width(), 20)

	mdInput := strings.TrimSpace(content)
	if strings.Contains(mdInput, "\\n") {
		mdInput = strings.ReplaceAll(mdInput, "\\n", "\n")
	}

	if rendered, ok := renderMarkdownPreview(mdInput, maxWidth); ok {
		return lipgloss.NewStyle().
			Foreground(m.styles.CommandHighlightStyle.GetForeground()).
			Width(maxWidth).
			Render(strings.TrimSpace(rendered))
	}

	lines := wordWrap(mdInput, maxWidth)
	if len(lines) == 0 {
		lines = []string{""}
	}
	return lipgloss.NewStyle().
		Foreground(m.styles.CommandHighlightStyle.GetForeground()).
		Width(maxWidth).
		Render(strings.Join(lines, "\n"))
}

func (m *CodeAgentModel) renderSystemMessage(msg ChatMessage) string {
	maxWidth := max(m.chatViewport.Width(), 20)
	lines := wordWrap(strings.TrimSpace(msg.Content), maxWidth-2)
	if len(lines) == 0 {
		lines = []string{""}
	}

	fg := lipgloss.Color("255")
	bg := lipgloss.Color("238")
	switch msg.SystemKind {
	case "error":
		bg = lipgloss.Color("160")
	case "done":
		bg = lipgloss.Color("28")
	case "working":
		bg = lipgloss.Color("25")
	default:
		bg = lipgloss.Color("60")
	}

	content := strings.Join(lines, "\n")
	return lipgloss.NewStyle().
		Width(maxWidth).
		Foreground(fg).
		Background(bg).
		Padding(0, 1).
		Render(content)
}

func (m *CodeAgentModel) renderToolMessage(msg ChatMessage) string {
	maxWidth := max(m.chatViewport.Width(), 20)
	state := msg.ToolState
	if state == "" {
		state = "running"
	}

	stateColor := m.styles.CommandHighlightStyle.GetForeground()
	switch state {
	case "error":
		stateColor = lipgloss.Color("9")
	case "done":
		stateColor = m.styles.SuccessStyle.GetForeground()
	case "running":
		stateColor = m.styles.CommandHighlightStyle.GetForeground()
	}

	toolName := strings.ToUpper(strings.TrimSpace(msg.ToolName))
	if toolName == "" {
		toolName = "TOOL"
	}
	header := lipgloss.NewStyle().Foreground(stateColor).Bold(true).Render(toolName)
	selected := msg.ToolCallID != "" && msg.ToolCallID == m.selectedToolCallID
	if selected {
		header = lipgloss.NewStyle().Foreground(stateColor).Bold(true).Render(toolName + "  [selected]")
	}

	meta := []string{}
	if strings.TrimSpace(msg.ToolPath) != "" {
		meta = append(meta, m.styles.MutedStyle.Render("path: "+msg.ToolPath))
	}
	if msg.ToolName == "bash" && strings.TrimSpace(msg.ToolCommand) != "" {
		meta = append(meta, m.styles.MutedStyle.Render("command: "+msg.ToolCommand))
	}

	body := strings.TrimSpace(msg.Content)
	if body == "" {
		body = "(no output yet)"
	}
	body = m.styleToolBody(msg.ToolName, state, body)
	wrapped := wrapMultiline(body, maxWidth-2)
	if len(wrapped) == 0 {
		wrapped = []string{""}
	}

	if msg.ToolName == "read" {
		if len(wrapped) > 10 {
			hidden := len(wrapped) - 10
			wrapped = append(wrapped[:10], m.styles.MutedStyle.Render(fmt.Sprintf("... (%d more lines)", hidden)))
		}
	} else {
		expanded := msg.ToolCallID != "" && m.toolExpanded[msg.ToolCallID]
		if !expanded {
			previewLines := m.toolPreviewLines()
			if len(wrapped) > previewLines {
				hidden := len(wrapped) - previewLines
				if msg.ToolName == "write" {
					wrapped = wrapped[:previewLines]
					wrapped = append(wrapped, m.styles.MutedStyle.Render(fmt.Sprintf("... (%d more lines, Ctrl+O to expand)", hidden)))
				} else {
					wrapped = wrapped[len(wrapped)-previewLines:]
					wrapped = append([]string{m.styles.MutedStyle.Render(fmt.Sprintf("... (%d earlier lines, Ctrl+O to expand)", hidden))}, wrapped...)
				}
			}
		}
	}

	blockLines := []string{header}
	blockLines = append(blockLines, meta...)
	blockLines = append(blockLines, strings.Join(wrapped, "\n"))

	if !msg.ToolStartedAt.IsZero() {
		end := msg.ToolEndedAt
		label := "Took"
		if end.IsZero() {
			end = time.Now()
			label = "Elapsed"
		}
		blockLines = append(blockLines, m.styles.MutedStyle.Render(fmt.Sprintf("%s %s", label, end.Sub(msg.ToolStartedAt).Round(time.Second))))
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1).
		Width(maxWidth)
	if selected {
		box = box.
			Background(lipgloss.Color("236")).
			BorderForeground(lipgloss.Color("15"))
	} else {
		box = box.BorderForeground(stateColor)
	}

	return box.Render(strings.Join(blockLines, "\n"))
}

func renderMarkdownPreview(content string, width int) (string, bool) {
	if !looksLikeMarkdown(content) {
		return "", false
	}
	if width <= 0 {
		width = 20
	}

	headStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	bulletStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	quoteStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Italic(true)
	codeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	inlineCodeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Background(lipgloss.Color("236"))

	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	inCode := false

	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trim := strings.TrimSpace(line)

		if strings.HasPrefix(trim, "```") {
			inCode = !inCode
			if inCode {
				lang := strings.TrimSpace(strings.TrimPrefix(trim, "```"))
				if lang != "" {
					out = append(out, codeStyle.Bold(true).Render("Code ("+lang+")"))
				} else {
					out = append(out, codeStyle.Bold(true).Render("Code"))
				}
			}
			continue
		}
		if inCode {
			wrapped := wordWrap(line, max(width-2, 20))
			for _, w := range wrapped {
				out = append(out, codeStyle.Render("  "+w))
			}
			continue
		}

		switch {
		case strings.HasPrefix(trim, "### "):
			out = append(out, headStyle.Render(strings.TrimSpace(strings.TrimPrefix(trim, "### "))))
			continue
		case strings.HasPrefix(trim, "## "):
			out = append(out, headStyle.Render(strings.TrimSpace(strings.TrimPrefix(trim, "## "))))
			continue
		case strings.HasPrefix(trim, "# "):
			out = append(out, headStyle.Render(strings.TrimSpace(strings.TrimPrefix(trim, "# "))))
			continue
		case strings.HasPrefix(trim, "- ") || strings.HasPrefix(trim, "* "):
			item := strings.TrimSpace(trim[2:])
			out = append(out, bulletStyle.Render("• ")+renderInlineCode(item, inlineCodeStyle))
			continue
		case strings.HasPrefix(trim, "> "):
			out = append(out, quoteStyle.Render(strings.TrimSpace(strings.TrimPrefix(trim, "> "))))
			continue
		}

		if len(trim) > 3 && trim[1] == '.' && trim[0] >= '0' && trim[0] <= '9' {
			parts := strings.SplitN(trim, ".", 2)
			if len(parts) == 2 {
				out = append(out, bulletStyle.Render(parts[0]+".")+" "+renderInlineCode(strings.TrimSpace(parts[1]), inlineCodeStyle))
				continue
			}
		}

		wrapped := wordWrap(line, max(width, 20))
		for _, w := range wrapped {
			out = append(out, lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Render(renderInlineCode(w, inlineCodeStyle)))
		}
	}

	return strings.Join(out, "\n"), true
}

func renderInlineCode(line string, style lipgloss.Style) string {
	if !strings.Contains(line, "`") {
		return line
	}
	parts := strings.Split(line, "`")
	if len(parts) < 3 {
		return line
	}
	var b strings.Builder
	for i, part := range parts {
		if i%2 == 1 {
			b.WriteString(style.Render(part))
		} else {
			b.WriteString(part)
		}
	}
	return b.String()
}

func (m *CodeAgentModel) styleToolBody(toolName, state, body string) string {
	if state == "error" {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(body)
	}
	switch toolName {
	case "write":
		lines := strings.Split(body, "\n")
		for i, line := range lines {
			trim := strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "+ "):
				lines[i] = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(line)
			case strings.HasPrefix(line, "- "):
				lines[i] = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(line)
			case strings.HasPrefix(trim, "File:") || strings.HasPrefix(trim, "Changed ranges") || strings.HasPrefix(trim, "--- line diff ---"):
				lines[i] = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true).Render(line)
			case strings.Contains(strings.ToLower(trim), "successfully wrote"):
				lines[i] = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true).Render(line)
			}
		}
		return strings.Join(lines, "\n")
	case "read":
		lines := strings.Split(body, "\n")
		for i, line := range lines {
			trim := strings.TrimSpace(line)
			if strings.HasPrefix(trim, "[") && strings.HasSuffix(trim, "]") {
				lines[i] = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(line)
			}
		}
		return strings.Join(lines, "\n")
	case "bash":
		if strings.Contains(body, "Full output:") {
			return lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(body)
		}
	}
	return body
}
