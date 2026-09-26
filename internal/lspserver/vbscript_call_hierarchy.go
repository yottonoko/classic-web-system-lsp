package lspserver

import (
	"context"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

func vbscriptIncomingCallsFromShard(parsed *core.ParsedDocument, item lsp.CallHierarchyItem) []lsp.CallHierarchyIncomingCall {
	return vbscriptIncomingCallsFromShardContext(context.Background(), parsed, item)
}

func vbscriptIncomingCallsFromShardContext(ctx context.Context, parsed *core.ParsedDocument, item lsp.CallHierarchyItem) []lsp.CallHierarchyIncomingCall {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	if parsed == nil {
		return nil
	}
	shard := vbscript.BuildReferenceShard(parsed)
	if ctx.Err() != nil {
		return nil
	}
	signatures := vbscript.BuildSignatures(parsed)
	if ctx.Err() != nil {
		return nil
	}
	postings := shard.PostingsFor(item.Name)
	var calls []lsp.CallHierarchyIncomingCall
	for _, group := range shard.CallGroupsFor(item.Name) {
		if ctx.Err() != nil {
			return nil
		}
		if group.Scope == "" || strings.EqualFold(group.Scope, item.Name) {
			continue
		}
		enclosing, ok := signatures[strings.ToLower(group.Scope)]
		if !ok {
			continue
		}
		for _, postingIndex := range group.PostingIndexes {
			if ctx.Err() != nil {
				return nil
			}
			if postingIndex < 0 || postingIndex >= len(postings) {
				continue
			}
			calls = append(calls, lsp.CallHierarchyIncomingCall{
				From:       callHierarchyItem(parsed.URI, enclosing),
				FromRanges: []lsp.Range{postings[postingIndex].Range},
			})
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	return calls
}

func vbscriptOutgoingCallsFromShard(parsed *core.ParsedDocument, item lsp.CallHierarchyItem) []lsp.CallHierarchyOutgoingCall {
	return vbscriptOutgoingCallsFromShardContext(context.Background(), parsed, item)
}

func vbscriptOutgoingCallsFromShardContext(ctx context.Context, parsed *core.ParsedDocument, item lsp.CallHierarchyItem) []lsp.CallHierarchyOutgoingCall {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	if parsed == nil {
		return nil
	}
	shard := vbscript.BuildReferenceShard(parsed)
	if ctx.Err() != nil {
		return nil
	}
	var calls []lsp.CallHierarchyOutgoingCall
	for _, target := range vbscript.Signatures(parsed) {
		if ctx.Err() != nil {
			return nil
		}
		if strings.EqualFold(target.Name, item.Name) {
			continue
		}
		postings := shard.PostingsFor(target.Name)
		for _, group := range shard.CallGroupsFor(target.Name) {
			if ctx.Err() != nil {
				return nil
			}
			if !strings.EqualFold(group.Scope, item.Name) {
				continue
			}
			for _, postingIndex := range group.PostingIndexes {
				if ctx.Err() != nil {
					return nil
				}
				if postingIndex < 0 || postingIndex >= len(postings) {
					continue
				}
				calls = append(calls, lsp.CallHierarchyOutgoingCall{
					To:         callHierarchyItem(parsed.URI, target),
					FromRanges: []lsp.Range{postings[postingIndex].Range},
				})
			}
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	return calls
}
