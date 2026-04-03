package tools

import (
	"path/filepath"
	"sync"
)

var (
	mutationMu    sync.Mutex
	mutationQueue = map[string]chan struct{}{}
)

// withFileMutationQueue serializes mutating operations for the same file key.
func withFileMutationQueue(path string, fn func() error) error {
	key := filepath.Clean(path)

	mutationMu.Lock()
	ch, ok := mutationQueue[key]
	if !ok {
		ch = make(chan struct{}, 1)
		ch <- struct{}{}
		mutationQueue[key] = ch
	}
	mutationMu.Unlock()

	<-ch
	defer func() { ch <- struct{}{} }()

	return fn()
}
