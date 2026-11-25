package server

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerDocumentResults(t *testing.T) {
	s := newTestServer(t, map[string][]byte{
		"main.xgo":  []byte("echo choice, 1\n"),
		"other.xgo": []byte("const choice = 2\n"),
		"data.json": []byte("{}"),
	})
	t.Cleanup(s.Close)
	doc := TextDocumentIdentifier{URI: "file:///main.xgo"}

	links, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: doc})
	require.NoError(t, err)
	require.NotEmpty(t, links)
	tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{TextDocument: doc})
	require.NoError(t, err)
	require.NotNil(t, tokens)
	slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: doc}})
	require.NoError(t, err)
	require.NotEmpty(t, slots)

	againLinks, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: doc})
	require.NoError(t, err)
	require.NotEmpty(t, againLinks)
	assert.Same(t, &links[0], &againLinks[0])
	againTokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{TextDocument: doc})
	require.NoError(t, err)
	assert.Same(t, tokens, againTokens)
	againSlots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: doc}})
	require.NoError(t, err)
	require.NotEmpty(t, againSlots)
	assert.Same(t, &slots[0], &againSlots[0])

	s.ModifyFiles([]FileChange{{Path: "other.xgo", Content: []byte("var choice int\n"), Version: 1}})
	changedLinks, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: doc})
	require.NoError(t, err)
	require.NotEmpty(t, changedLinks)
	assert.NotSame(t, &links[0], &changedLinks[0])
	changedTokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{TextDocument: doc})
	require.NoError(t, err)
	require.NotNil(t, changedTokens)
	assert.NotEqual(t, tokens.Data, changedTokens.Data)
	changedSlots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: doc}})
	require.NoError(t, err)
	require.NotEmpty(t, changedSlots)
	assert.NotSame(t, &slots[0], &changedSlots[0])

	for _, uri := range []DocumentURI{"file:///data.json", "file:///missing.xgo"} {
		links, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: TextDocumentIdentifier{URI: uri}})
		assert.NoError(t, err)
		assert.Nil(t, links)
	}
}

func TestDocumentResultGet(t *testing.T) {
	var cached documentResult[*int]
	var builds atomic.Int32
	result := new(1)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			got, err := cached.get(func() (*int, error) {
				builds.Add(1)
				return result, nil
			})
			assert.NoError(t, err)
			assert.Same(t, result, got)
		})
	}
	wg.Wait()
	assert.EqualValues(t, 1, builds.Load())
}
