package classfile

import (
	"github.com/goplus/xgolsw/internal/analysis"
	"github.com/goplus/xgolsw/xgo"
)

// Context carries shared dependencies from the language server into a provider.
type Context struct {
	Project    *xgo.Project
	Translator func(string) string
	Analyzers  []*analysis.Analyzer
}
