package server

import (
	"sync"

	"github.com/goplus/xgolsw/xgo"
)

// documentCacheKind identifies request results for a file in an analysis snapshot.
type documentCacheKind struct{}

// documentCache holds immutable results computed on demand for a document.
type documentCache struct {
	links  documentResult[[]DocumentLink]
	slots  documentResult[[]XGoInputSlot]
	tokens documentResult[*SemanticTokens]
}

// documentResult computes a single document result and shares it across requests.
type documentResult[T any] struct {
	init sync.Once
	load func() (T, error)
}

// get returns the result of the first collection for this snapshot.
func (r *documentResult[T]) get(collect func() (T, error)) (T, error) {
	r.init.Do(func() { r.load = sync.OnceValues(collect) })
	return r.load()
}

// buildDocumentCache creates an empty cache for a file.
func buildDocumentCache(*xgo.Project, string, *xgo.File) (any, error) {
	return new(documentCache), nil
}

// requestDocument returns the current snapshot and cache for a source file.
func (s *Server) requestDocument(filename string) (*xgo.Project, *documentCache) {
	proj := s.syncProject()
	if !proj.IsSourceFile(filename) {
		return proj, nil
	}
	data, err := proj.FileCache(documentCacheKind{}, filename)
	if err != nil {
		return proj, nil
	}
	return proj, data.(*documentCache)
}
