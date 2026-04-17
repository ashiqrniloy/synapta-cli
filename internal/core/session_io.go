package core

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func NewSessionStore(baseDir, agentID, cwd string, settings CompactionSettings) (*SessionStore, error) {
	s := &SessionStore{
		baseDir:    baseDir,
		agentID:    agentID,
		cwd:        cwd,
		sessionDir: sessionDirFor(baseDir, agentID, cwd),
		settings:   settings,
	}
	if err := os.MkdirAll(s.sessionDir, 0755); err != nil {
		return nil, fmt.Errorf("creating sessions dir: %w", err)
	}

	recent, err := findMostRecentSessionFile(s.sessionDir)
	if err != nil {
		return nil, err
	}
	if recent == "" {
		if err := s.startNewLocked(); err != nil {
			return nil, err
		}
		return s, nil
	}

	s.filePath = recent
	if err := s.loadFromFileLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

func OpenSessionStore(baseDir, agentID, cwd, sessionPath string, settings CompactionSettings) (*SessionStore, error) {
	if strings.TrimSpace(sessionPath) == "" {
		return nil, fmt.Errorf("session path is required")
	}
	absPath, err := filepath.Abs(sessionPath)
	if err != nil {
		return nil, fmt.Errorf("resolve session path: %w", err)
	}
	s := &SessionStore{
		baseDir:    baseDir,
		agentID:    agentID,
		cwd:        cwd,
		sessionDir: sessionDirFor(baseDir, agentID, cwd),
		filePath:   absPath,
		settings:   settings,
	}
	if err := s.loadFromFileLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *SessionStore) StartNewSession() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startNewLocked()
}

func (s *SessionStore) startNewLocked() error {
	if err := s.closeAppendFileLocked(); err != nil {
		return err
	}
	timestamp := time.Now().UTC()
	s.sessionID = fmt.Sprintf("%d", timestamp.UnixNano())
	fileTime := strings.NewReplacer(":", "-", ".", "-").Replace(timestamp.Format(time.RFC3339Nano))
	s.filePath = filepath.Join(s.sessionDir, fileTime+"_"+s.sessionID+".jsonl")

	header := sessionEntry{
		Type:      sessionEntryTypeSession,
		Version:   currentSessionVersion,
		ID:        s.sessionID,
		Timestamp: timestamp,
		CWD:       s.cwd,
	}
	s.entries = []sessionEntry{header}
	return s.rewriteAllLocked()
}

func (s *SessionStore) OpenSession(sessionPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	absPath, err := filepath.Abs(sessionPath)
	if err != nil {
		return fmt.Errorf("resolve session path: %w", err)
	}
	s.filePath = absPath
	return s.loadFromFileLocked()
}

func (s *SessionStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeAppendFileLocked()
}

func (s *SessionStore) loadFromFileLocked() error {
	if err := s.closeAppendFileLocked(); err != nil {
		return err
	}

	f, err := os.Open(s.filePath)
	if err != nil {
		return fmt.Errorf("open session file: %w", err)
	}
	defer f.Close()

	s.entries = make([]sessionEntry, 0)
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
		s.entries = append(s.entries, e)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan session file: %w", err)
	}
	if len(s.entries) == 0 || s.entries[0].Type != sessionEntryTypeSession {
		return fmt.Errorf("invalid session file: missing session header")
	}
	header := s.entries[0]
	s.sessionID = header.ID
	if strings.TrimSpace(header.CWD) != "" {
		s.cwd = header.CWD
	}
	return nil
}

func (s *SessionStore) SessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionID
}

func (s *SessionStore) SessionFile() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.filePath
}

func (s *SessionStore) CWD() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cwd
}

func (s *SessionStore) appendEntryLocked(e sessionEntry) error {
	if err := s.ensureAppendFileLocked(); err != nil {
		return err
	}
	line, err := encodeSessionEntryJSONLine(e)
	if err != nil {
		return err
	}
	if _, err := s.appendFile.WriteString(line + "\n"); err != nil {
		_ = s.closeAppendFileLocked()
		return fmt.Errorf("append session entry: %w", err)
	}
	return nil
}

func (s *SessionStore) ensureAppendFileLocked() error {
	if s.appendFile != nil {
		return nil
	}
	f, err := os.OpenFile(s.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open session file append: %w", err)
	}
	s.appendFile = f
	return nil
}

func (s *SessionStore) closeAppendFileLocked() error {
	if s.appendFile == nil {
		return nil
	}
	if err := s.appendFile.Close(); err != nil {
		s.appendFile = nil
		return fmt.Errorf("close session append file: %w", err)
	}
	s.appendFile = nil
	return nil
}

func (s *SessionStore) rewriteAllLocked() error {
	if err := s.closeAppendFileLocked(); err != nil {
		return err
	}

	f, err := os.Create(s.filePath)
	if err != nil {
		return fmt.Errorf("create session file: %w", err)
	}
	defer f.Close()
	for _, e := range s.entries {
		line, err := encodeSessionEntryJSONLine(e)
		if err != nil {
			return err
		}
		if _, err := f.WriteString(line + "\n"); err != nil {
			return fmt.Errorf("write session entry: %w", err)
		}
	}
	return nil
}
