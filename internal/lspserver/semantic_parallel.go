package lspserver

import (
	"context"
	"sync"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

type semanticTokensGeneration struct {
	parsedRevision               uint64
	javascriptDocumentGeneration uint64
	javascriptMappingGeneration  uint64
}

type semanticTokensInflight struct {
	mu           sync.Mutex
	done         chan struct{}
	tokens       lsp.SemanticTokens
	ctx          context.Context
	cancel       context.CancelFunc
	completeOnce sync.Once
	completed    bool
	uri          string
	document     *core.TextDocument
	version      int
	text         string
	generation   semanticTokensGeneration
}

func newSemanticTokensInflight() *semanticTokensInflight {
	ctx, cancel := context.WithCancel(context.Background())
	return &semanticTokensInflight{done: make(chan struct{}), ctx: ctx, cancel: cancel}
}

func (f *semanticTokensInflight) setTokens(tokens lsp.SemanticTokens) bool {
	if f == nil {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.completed {
		return false
	}
	f.tokens = tokens
	return true
}

func (f *semanticTokensInflight) snapshot() lsp.SemanticTokens {
	if f == nil {
		return lsp.SemanticTokens{}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokens
}

func (f *semanticTokensInflight) wait(ctx context.Context) bool {
	if f == nil {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	f.mu.Lock()
	if f.done == nil {
		f.done = make(chan struct{})
	}
	done := f.done
	f.mu.Unlock()
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}

func (f *semanticTokensInflight) active() bool {
	if f == nil {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.completed
}

func (f *semanticTokensInflight) context() context.Context {
	if f == nil {
		return context.Background()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ctx == nil {
		f.ctx = context.Background()
	}
	return f.ctx
}

func (f *semanticTokensInflight) complete(tokens lsp.SemanticTokens) {
	if f == nil {
		return
	}
	f.completeOnce.Do(func() {
		f.mu.Lock()
		if f.done == nil {
			f.done = make(chan struct{})
		}
		f.tokens = tokens
		f.completed = true
		done := f.done
		cancel := f.cancel
		f.mu.Unlock()
		close(done)
		if cancel != nil {
			cancel()
		}
	})
}

func (f *semanticTokensInflight) invalidate() {
	if f == nil {
		return
	}
	f.completeOnce.Do(func() {
		f.mu.Lock()
		if f.done == nil {
			f.done = make(chan struct{})
		}
		f.completed = true
		done := f.done
		cancel := f.cancel
		f.mu.Unlock()
		close(done)
		if cancel != nil {
			cancel()
		}
	})
}
