package server

import (
	"go/constant"
	gotoken "go/token"
	gotypes "go/types"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const xgoUnitCompletionSource = `import (
	"time"
	"example.com/unit"
)

type Options struct {
	Delay time.Duration
}

func wait(d time.Duration) {}
func move(d unit.Distance) {}
func configure(opts Options?) {}

onStart => {
	wait 1
	wait 1m
	move 1m
	configure delay = 1
}
`

func newXGoUnitTestServer(source string) *Server {
	m := map[string][]byte{
		"main.spx":          []byte(source),
		"assets/index.json": []byte(`{}`),
	}
	proj := newProjectWithoutModTime(m)
	s := New(proj, nil, fileMapGetter(m), &MockScheduler{})
	s.workspaceRootFS.Importer = xgoUnitTestImporter{fallback: s.workspaceRootFS.Importer}
	return s
}

type xgoUnitTestImporter struct {
	fallback gotypes.Importer
}

func (i xgoUnitTestImporter) Import(path string) (*gotypes.Package, error) {
	if path == "example.com/unit" {
		pkg := gotypes.NewPackage(path, "unit")
		distanceObj := gotypes.NewTypeName(gotoken.NoPos, pkg, "Distance", nil)
		gotypes.NewNamed(distanceObj, gotypes.Typ[gotypes.Int], nil)
		pkg.Scope().Insert(distanceObj)
		secondsObj := gotypes.NewTypeName(gotoken.NoPos, pkg, "Seconds", nil)
		gotypes.NewAlias(secondsObj, gotypes.Typ[gotypes.Float64])
		pkg.Scope().Insert(secondsObj)
		metersObj := gotypes.NewTypeName(gotoken.NoPos, pkg, "Meters", nil)
		gotypes.NewAlias(metersObj, gotypes.Typ[gotypes.Float64])
		pkg.Scope().Insert(metersObj)
		pkg.Scope().Insert(gotypes.NewConst(
			gotoken.NoPos,
			pkg,
			"XGou_Distance",
			gotypes.Typ[gotypes.UntypedString],
			constant.MakeString("mm=1,cm=10,m=1000"),
		))
		pkg.Scope().Insert(gotypes.NewConst(
			gotoken.NoPos,
			pkg,
			"XGou_Seconds",
			gotypes.Typ[gotypes.UntypedString],
			constant.MakeString("s=1,ms=0.001"),
		))
		pkg.Scope().Insert(gotypes.NewConst(
			gotoken.NoPos,
			pkg,
			"XGou_Meters",
			gotypes.Typ[gotypes.UntypedString],
			constant.MakeString("m=1,km=1000"),
		))
		pkg.MarkComplete()
		return pkg, nil
	}
	return i.fallback.Import(path)
}

func assertCompletionItemTextEdit(t *testing.T, items []CompletionItem, label string, want TextEdit) {
	t.Helper()

	item := completionItemByLabel(items, label)
	require.NotNil(t, item)
	require.Equal(t, UnitCompletion, item.Kind)
	require.NotNil(t, item.TextEdit)
	assert.Equal(t, want, item.TextEdit.Value)
}

func completionItemByLabel(items []CompletionItem, label string) *CompletionItem {
	for i := range items {
		if items[i].Label == label {
			return &items[i]
		}
	}
	return nil
}

func completionItemLabels(items []CompletionItem) []string {
	labels := make([]string, 0, len(items))
	for _, item := range items {
		labels = append(labels, item.Label)
	}
	return labels
}

func containsCompletionItemKind(items []CompletionItem, kind CompletionItemKind) bool {
	for _, item := range items {
		if item.Kind == kind {
			return true
		}
	}
	return false
}
