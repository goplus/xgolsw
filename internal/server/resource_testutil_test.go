package server

import "net/url"

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

func newTestResourceAnalysis(ids ...resourceID) *resourceAnalysis {
	existing := make(map[resourceID]bool, len(ids))
	for _, id := range ids {
		existing[id] = true
	}
	return &resourceAnalysis{
		contains: func(id resourceID) bool { return existing[id] },
	}
}

func resourceDiagnostics(s *Server, analysis *resourceAnalysis) diagnosticResult {
	result := newDiagnosticResult()
	s.collectResourceDiagnostics(&result, analysis)
	return result
}
