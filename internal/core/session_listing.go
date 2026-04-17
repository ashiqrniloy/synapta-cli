package core

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func ListAllSessions(baseDir, agentID string) ([]SessionInfo, error) {
	agentSessionsDir := filepath.Join(baseDir, "sessions", agentID)
	if _, err := os.Stat(agentSessionsDir); err != nil {
		if os.IsNotExist(err) {
			return []SessionInfo{}, nil
		}
		return nil, fmt.Errorf("stat sessions dir: %w", err)
	}

	dirs, err := os.ReadDir(agentSessionsDir)
	if err != nil {
		return nil, fmt.Errorf("read sessions dir: %w", err)
	}

	all := make([]SessionInfo, 0)
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		infos, err := listSessionsFromDir(filepath.Join(agentSessionsDir, d.Name()))
		if err != nil {
			continue
		}
		all = append(all, infos...)
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].Modified.After(all[j].Modified)
	})
	return all, nil
}

func listSessionsFromDir(dir string) ([]SessionInfo, error) {
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return []SessionInfo{}, nil
		}
		return nil, fmt.Errorf("stat sessions dir: %w", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read sessions dir: %w", err)
	}

	infos := make([]SessionInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		info, err := buildSessionInfo(path)
		if err != nil {
			continue
		}
		infos = append(infos, info)
	}

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Modified.After(infos[j].Modified)
	})
	return infos, nil
}

func sessionDirFor(baseDir, agentID, cwd string) string {
	return filepath.Join(baseDir, "sessions", agentID, encodeCWD(cwd))
}

func encodeCWD(cwd string) string {
	repl := strings.NewReplacer("/", "-", "\\", "-", ":", "-", " ", "_")
	encoded := repl.Replace(strings.TrimSpace(cwd))
	if encoded == "" {
		return "default"
	}
	return strings.Trim(encoded, "-")
}

func findMostRecentSessionFile(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read sessions dir: %w", err)
	}

	type candidate struct {
		path    string
		modTime time.Time
	}
	candidates := make([]candidate, 0, len(entries))

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if !isValidSessionFile(path) {
			continue
		}
		st, err := os.Stat(path)
		if err != nil {
			continue
		}
		candidates = append(candidates, candidate{path: path, modTime: st.ModTime()})
	}
	if len(candidates) == 0 {
		return "", nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modTime.After(candidates[j].modTime)
	})
	return candidates[0].path, nil
}

func isValidSessionFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return false
	}
	line := strings.TrimSpace(scanner.Text())
	if line == "" {
		return false
	}
	e, err := decodeSessionEntryJSONLine(line)
	if err != nil {
		return false
	}
	return e.Type == sessionEntryTypeSession && strings.TrimSpace(e.ID) != ""
}

func buildSessionInfo(path string) (SessionInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return SessionInfo{}, err
	}
	defer f.Close()

	entries := make([]sessionEntry, 0, 64)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		e, err := decodeSessionEntryJSONLine(line)
		if err != nil {
			continue
		}
		entries = append(entries, e)
	}
	if err := scanner.Err(); err != nil {
		return SessionInfo{}, err
	}
	if len(entries) == 0 || entries[0].Type != sessionEntryTypeSession {
		return SessionInfo{}, fmt.Errorf("invalid session file")
	}

	header := entries[0]
	created := header.Timestamp
	if created.IsZero() {
		created = time.Now()
	}

	messageCount := 0
	firstMessage := ""
	for _, e := range entries {
		if e.Type != sessionEntryTypeMessage || e.Message == nil {
			continue
		}
		if e.Message.Role != "user" && e.Message.Role != "assistant" {
			continue
		}
		messageCount++
		if firstMessage == "" && e.Message.Role == "user" {
			firstMessage = strings.TrimSpace(e.Message.Content)
		}
	}
	if firstMessage == "" {
		firstMessage = "(no messages)"
	}

	st, err := os.Stat(path)
	if err != nil {
		return SessionInfo{}, err
	}
	modified := st.ModTime()

	return SessionInfo{
		Path:         path,
		ID:           header.ID,
		CWD:          header.CWD,
		Created:      created,
		Modified:     modified,
		MessageCount: messageCount,
		FirstMessage: firstMessage,
	}, nil
}
