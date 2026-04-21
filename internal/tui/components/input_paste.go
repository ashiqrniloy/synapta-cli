package components

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// pasteSizeThreshold is the byte length above which pasted text is offloaded
// to a persistent file instead of being inserted directly into the textarea.
// 5 KB is large enough to hold a normal message but small enough to avoid
// the layout-recalculation lag that comes with many textarea lines.
const pasteSizeThreshold = 5 * 1024 // 5 KB

// pasteTempDir returns the persistent directory used to store large-paste
// files for the current session.  It lives inside the agent dir so that it
// survives OS reboots (unlike os.TempDir()/tmp).
//
//	~/.synapta/paste-tmp/
func pasteTempDir(agentDir string) string {
	return filepath.Join(agentDir, "paste-tmp")
}

// handlePaste is the single entry-point for all paste events (tea.PasteMsg).
//
// Why this matters for performance:
//   - Without bracketed-paste support every pasted character arrives as a
//     separate tea.KeyPressMsg, so a 10 KB paste generates ~10 000 update
//     cycles each calling ta.Update() + recalculateLayout().
//   - Bubbletea v2 delivers the entire pasted block as one tea.PasteMsg,
//     giving us one update cycle regardless of content length.
//
// For text above pasteSizeThreshold the content is written to a persistent
// file under agentDir/paste-tmp/ and an attachment token `<path>` is appended
// to the textarea instead.  The existing expandAttachmentTokens() machinery in
// handleSubmitKeyPress() inlines the file content at send-time, and the fully
// expanded text (not the token) is what gets persisted in the session JSONL.
func (m *CodeAgentModel) handlePaste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	content := msg.Content

	// Modals own the input — forward normally without large-paste offloading.
	if m.contextModalOpen || m.commandModalOpen || m.keybindingsModalOpen || m.fileBrowserModalOpen {
		var cmd tea.Cmd
		if m.commandModalOpen {
			m.commandModalInput, cmd = m.commandModalInput.Update(msg)
		} else if m.contextModalOpen && m.contextModalEditMode {
			m.contextModalEditor, cmd = m.contextModalEditor.Update(msg)
		} else {
			m.ta, cmd = m.ta.Update(msg)
		}
		m.recalculateLayout()
		return m, cmd
	}

	// ── Large-paste path ────────────────────────────────────────────────────
	if len(content) > pasteSizeThreshold {
		return m.handleLargePaste(content)
	}

	// ── Normal paste path ───────────────────────────────────────────────────
	// One ta.Update() call + one recalculateLayout() — O(1) layout cost.
	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	m.recalculateLayout()
	return m, cmd
}

// handleLargePaste saves content to a persistent file under agentDir/paste-tmp/
// and inserts a compact backtick attachment token into the textarea, e.g.:
//
//	`/home/user/.synapta/paste-tmp/synapta-paste-abc123.txt`
//
// At send-time expandAttachmentTokens() reads the file and inlines the content
// so the session JSONL always stores the full text — not a path reference.
// After the message is successfully persisted, cleanupPasteTempFile() removes
// the file so the directory doesn't grow indefinitely.
func (m *CodeAgentModel) handleLargePaste(content string) (tea.Model, tea.Cmd) {
	path, err := m.writePasteTempFile(content)
	if err != nil {
		m.appendSystemMessage(
			fmt.Sprintf("[Paste] ✗ Could not save large paste to file: %v", err),
			"error",
		)
		// Graceful fallback: insert directly so the user doesn't lose content.
		m.ta.InsertString(content)
		m.recalculateLayout()
		return m, nil
	}

	// Append the attachment token after any existing textarea content.
	existing := m.ta.Value()
	sep := ""
	if existing != "" && !strings.HasSuffix(existing, " ") && !strings.HasSuffix(existing, "\n") {
		sep = " "
	}
	m.ta.SetValue(existing + sep + "`" + path + "`")
	m.ta.CursorEnd()

	lineCount := strings.Count(content, "\n") + 1
	m.appendSystemMessage(
		fmt.Sprintf("[Paste] Large paste (%d lines, %s) saved — will be inlined on send",
			lineCount, humanBytes(len(content))),
		"info",
	)

	m.recalculateLayout()
	return m, nil
}

// writePasteTempFile writes content to a new file in the persistent
// agentDir/paste-tmp/ directory and returns the absolute path.
// Using the agent dir (not os.TempDir) ensures the file survives reboots,
// so a user can paste, close the terminal, reopen, and still submit successfully.
func (m *CodeAgentModel) writePasteTempFile(content string) (string, error) {
	dir := pasteTempDir(m.agentDir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create paste-tmp dir: %w", err)
	}
	f, err := os.CreateTemp(dir, "synapta-paste-*.txt")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		return "", err
	}
	return f.Name(), nil
}

// cleanupPasteTempFile removes a paste temp file after its content has been
// successfully inlined and persisted into the session JSONL.
// Errors are ignored — a stale file in paste-tmp is harmless.
func cleanupPasteTempFile(path string) {
	if strings.Contains(filepath.Base(path), "synapta-paste-") {
		_ = os.Remove(path)
	}
}

// humanBytes formats a byte count as a human-readable string (e.g. "12 KB").
func humanBytes(n int) string {
	switch {
	case n >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	case n >= 1024:
		return fmt.Sprintf("%d KB", n/1024)
	default:
		return fmt.Sprintf("%d B", n)
	}
}
