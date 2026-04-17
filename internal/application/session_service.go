package application

import (
	"context"
	"fmt"

	"github.com/ashiqrniloy/synapta-cli/internal/core"
	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

type SessionService struct {
	agentDir       string
	agentID        string
	chatController *ChatController
	contextManager *core.ContextManager
	sessionStore   *core.SessionStore
}

func NewSessionService(agentDir string, agentID string, chatController *ChatController, contextManager *core.ContextManager, sessionStore *core.SessionStore) *SessionService {
	return &SessionService{
		agentDir:       agentDir,
		agentID:        agentID,
		chatController: chatController,
		contextManager: contextManager,
		sessionStore:   sessionStore,
	}
}

func (s *SessionService) SetSessionStore(store *core.SessionStore) {
	s.sessionStore = store
}

func (s *SessionService) SetChatController(chatController *ChatController) {
	s.chatController = chatController
}

func (s *SessionService) SetContextManager(contextManager *core.ContextManager) {
	s.contextManager = contextManager
}

func (s *SessionService) ListAll() ([]core.SessionInfo, error) {
	return core.ListAllSessions(s.agentDir, s.agentID)
}

func (s *SessionService) StartNew(cwd string) (*core.SessionStore, string, error) {
	store := s.sessionStore
	if store == nil {
		var err error
		store, err = core.NewSessionStore(s.agentDir, s.agentID, cwd, core.DefaultCompactionSettings())
		if err != nil {
			return nil, "", err
		}
	}
	if err := store.StartNewSession(); err != nil {
		return nil, "", err
	}
	s.sessionStore = store
	return store, store.SessionID(), nil
}

func (s *SessionService) Resume(cwd, sessionPath string) (*core.SessionStore, error) {
	store, err := core.OpenSessionStore(s.agentDir, s.agentID, cwd, sessionPath, core.DefaultCompactionSettings())
	if err != nil {
		return nil, err
	}
	s.sessionStore = store
	return store, nil
}

func (s *SessionService) ManualCompact(ctx context.Context, providerID, modelID string) (bool, []llm.Message, core.CompactionMethod, error) {
	if s.sessionStore == nil {
		return false, nil, "", fmt.Errorf("session store not available")
	}

	contextWindow := 128000
	if s.chatController != nil && providerID != "" && modelID != "" {
		if cw, err := s.chatController.ModelContextWindow(ctx, providerID, modelID); err == nil && cw > 0 {
			contextWindow = cw
		}
	}

	summarizer := func(ctx context.Context, toSummarize []llm.Message, previousSummary string) (string, error) {
		if s.chatController == nil || providerID == "" || modelID == "" {
			return "", nil
		}
		messagesForSummary := toSummarize
		if s.contextManager != nil {
			if built, err := s.contextManager.Build(toSummarize); err == nil && len(built) > 0 {
				messagesForSummary = built
			}
		}
		return s.chatController.SummarizeCompaction(ctx, providerID, modelID, messagesForSummary, previousSummary)
	}

	compacted, method, err := s.sessionStore.ManualCompact(ctx, contextWindow, summarizer)
	if err != nil {
		return false, nil, "", err
	}
	return compacted, s.sessionStore.ContextMessages(), method, nil
}
