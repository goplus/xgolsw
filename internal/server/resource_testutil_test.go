package server

import (
	"net/url"

	"github.com/goplus/xgolsw/xgo"
)

type testResourceID struct {
	collection string
	name       string
}

func (id testResourceID) Name() string { return id.name }
func (id testResourceID) URI() XGoResourceURI {
	return XGoResourceURI(string(id.ContextURI()) + "/" + url.PathEscape(id.name))
}
func (id testResourceID) ContextURI() XGoResourceContextURI {
	return XGoResourceContextURI("test://resources/" + id.collection)
}

func newTestResourceAnalysis(proj *xgo.Project, ids ...resourceID) *resourceAnalysis {
	existing := make(map[resourceID]bool, len(ids))
	for _, id := range ids {
		existing[id] = true
	}
	return &resourceAnalysis{
		proj:             proj,
		diagnosticResult: newDiagnosticResult(),
		contains:         func(id resourceID) bool { return existing[id] },
	}
}
