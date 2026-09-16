package server

import (
	gotypes "go/types"
	"io/fs"
	"strings"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompletionContextCollectPropertyNames(t *testing.T) {
	t.Run("SourceKinds", func(t *testing.T) {
		for _, tt := range []struct {
			name      string
			filename  string
			target    string
			source    string
			newServer testServerFactory
		}{
			{"XGo", "main.xgo", "Record", "type Record struct {\nscore int\nready bool\n}\nfunc (r *Record) Label() string { return \"item\" }\n", newTestServer},
			{"StandaloneClass", "Record.gox", "Record", "var (\nscore int\nready bool\n)\nfunc Label() string { return \"item\" }\n", newTestServer},
			{"ProjectClass", "main_fixture.gox", "App", "var (\nscore int\nready bool\n)\nfunc Label() string { return \"item\" }\n", newFrameworkTestServer},
			{"WorkClass", "Worker_fixture.gox", "Worker", "var (\nscore int\nready bool\n)\n", newFrameworkTestServer},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{tt.filename: []byte(tt.source)}
				if tt.name == "WorkClass" {
					files["main_fixture.gox"] = nil
				}
				s := tt.newServer(t, files)
				info, err := s.getProj().TypeInfo()
				require.NoError(t, err)
				ctx := &completionContext{
					definitionContext: definitionContext{proj: s.getProj(), lookupPkgDoc: s.lookupPkgDoc},
					typeInfo:          info, itemSet: newCompletionItemSet(Markdown),
				}
				// Candidate strings remain valid regardless of each property's value type.
				ctx.itemSet.setExpectedTypes([]gotypes.Type{gotypes.Typ[gotypes.String]})
				ctx.collectPropertyNames(tt.target)
				ctx.collectPropertyNames(tt.target)
				assert.ElementsMatch(t, []string{`"score"`, `"ready"`, `"label"`}, completionItemLabels(ctx.itemSet.items))
				for _, item := range ctx.itemSet.items {
					assert.Equal(t, PropertyCompletion, item.Kind)
					assert.Equal(t, item.Label, item.InsertText)
					assert.Equal(t, ToPtr(PlainTextTextFormat), item.InsertTextFormat)
					assert.Nil(t, item.TextEdit)
					data := requireValueAs[*CompletionItemData](t, item.Data)
					require.NotNil(t, data.Definition)
					if item.Label == `"label"` && tt.name == "WorkClass" {
						assert.Equal(t, "xgo:example.com/framework?Item.label", data.Definition.String())
						doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
						assert.Contains(t, doc.Value, "Label is exposed as a property in XGo source.")
					} else {
						assert.Equal(t, ToPtr("main"), data.Definition.Package)
					}
				}
			})
		}
	})

	t.Run("EmbeddedMembersAndAliases", func(t *testing.T) {
		s := newFrameworkTestServer(t, map[string][]byte{"main.xgo": []byte(`import "example.com/framework"
type Base struct {
    // Score stores the base score.
    Score int
    Shared int
}
func (b *Base) Label() string { return "base" }
type Record struct {
    *Base
    framework.Item
    Shared string
}
// Label describes the record.
func (r *Record) Label() string { return "record" }
func (r *Record) Reset() {}
func (r *Record) WithParam(v int) int { return v }
type RecordAlias = Record
type RecordPointer = *Record
`)})
		info, err := s.getProj().TypeInfo()
		require.NoError(t, err)
		for _, target := range []string{"Record", "RecordAlias", "RecordPointer"} {
			ctx := &completionContext{
				definitionContext: definitionContext{proj: s.getProj(), lookupPkgDoc: s.lookupPkgDoc},
				typeInfo:          info, itemSet: newCompletionItemSet(PlainText),
			}
			ctx.collectPropertyNames(target)
			require.Len(t, ctx.itemSet.items, 3)
			for _, want := range []struct {
				label string
				id    string
				doc   string
			}{
				{`"Score"`, "xgo:main?Base.Score", "Score stores the base score."},
				{`"Shared"`, "xgo:main?Record.Shared", ""},
				{`"label"`, "xgo:main?Record.Label", "Label describes the record."},
			} {
				item := completionItemByLabel(ctx.itemSet.items, want.label)
				require.NotNil(t, item)
				data := requireValueAs[*CompletionItemData](t, item.Data)
				assert.Equal(t, want.id, data.Definition.String())
				doc := requireValueAs[string](t, item.Documentation.Value)
				assert.Contains(t, doc, want.doc)
				assert.NotContains(t, doc, "<pre")
			}
		}
	})

	t.Run("InvalidTargets", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte("type Count int\ntype Alias = int\nvar count int\n")})
		info, err := s.getProj().TypeInfo()
		require.NoError(t, err)
		for _, target := range []string{"Missing", "Count", "Alias", "count"} {
			ctx := &completionContext{
				definitionContext: definitionContext{proj: s.getProj(), lookupPkgDoc: s.lookupPkgDoc},
				typeInfo:          info, itemSet: newCompletionItemSet(Markdown),
			}
			ctx.collectPropertyNames(target)
			assert.Empty(t, ctx.itemSet.items, target)
		}
	})

	t.Run("SourceAndDocumentationUpdates", func(t *testing.T) {
		s := newFrameworkTestServer(t, map[string][]byte{"Worker_fixture.gox": []byte("var before int\n"), "main_fixture.gox": nil})
		lookup := s.lookupPkgDoc
		for _, missing := range []bool{false, true, false} {
			info, err := s.getProj().TypeInfo()
			require.NoError(t, err)
			ctx := &completionContext{
				definitionContext: definitionContext{proj: s.getProj(), lookupPkgDoc: lookup},
				typeInfo:          info, itemSet: newCompletionItemSet(PlainText),
			}
			if missing {
				ctx.lookupPkgDoc = func(string) (*pkgdoc.PkgDoc, error) { return nil, fs.ErrNotExist }
			}
			ctx.collectPropertyNames("Worker")
			item := completionItemByLabel(ctx.itemSet.items, `"label"`)
			require.NotNil(t, item)
			doc := requireValueAs[string](t, item.Documentation.Value)
			assert.Equal(t, !missing, strings.Contains(doc, "Label is exposed as a property in XGo source."))
		}
		s.ModifyFiles([]FileChange{{Path: "Worker_fixture.gox", Content: []byte("var after bool\n"), Version: 1}})
		info, err := s.getProj().TypeInfo()
		require.NoError(t, err)
		ctx := &completionContext{
			definitionContext: definitionContext{proj: s.getProj(), lookupPkgDoc: lookup},
			typeInfo:          info, itemSet: newCompletionItemSet(Markdown),
		}
		ctx.collectPropertyNames("Worker")
		assert.ElementsMatch(t, []string{`"after"`, `"label"`}, completionItemLabels(ctx.itemSet.items))
	})

	t.Run("InsideStringLiteral", func(t *testing.T) {
		for _, literal := range []string{`"s"`, "`s`", `"s`} {
			source := "var score int\necho " + literal
			s := newFrameworkTestServer(t, map[string][]byte{"main_fixture.gox": []byte(source)})
			ctx := newCompletionTestContext(t, s, "main_fixture.gox", Position{Line: 1, Character: 7})
			ctx.collectPropertyNames("App")
			require.Len(t, ctx.itemSet.items, 1)
			item := ctx.itemSet.items[0]
			assert.Equal(t, "score", item.Label)
			assert.Equal(t, "score", item.InsertText)
			assert.Equal(t, string(literal[0])+"score"+string(literal[0]), item.FilterText)
			assert.Equal(t, PropertyCompletion, item.Kind)
			require.NotNil(t, item.TextEdit)
			edit := requireValueAs[TextEdit](t, item.TextEdit.Value)
			assert.Equal(t, Range{Start: Position{Line: 1, Character: 5}, End: Position{Line: 1, Character: uint32(5 + len(literal))}}, edit.Range)
			want := `"score"`
			if literal[0] == '`' {
				want = "`score`"
			}
			assert.Equal(t, want, edit.NewText)
		}
	})
}

func TestCompletionContextGetPropertyTarget(t *testing.T) {
	for _, tt := range []struct {
		name, source, want, wantCompletion string
		invalid                            bool
	}{
		{name: "ImplicitReceiver", source: "read 1", want: "ActorWorker", wantCompletion: "ActorWorker"},
		{name: "Value", source: "var item Record\nitem.show()", want: "Record", wantCompletion: "Record"},
		{name: "ParenthesizedMethod", source: "var item Record\n((item.show))()", want: "Record", wantCompletion: "Record"},
		{name: "Pointer", source: "var item *Record\nitem.show()", want: "Record", wantCompletion: "Record"},
		{name: "Alias", source: "type Alias = *Record\nvar item Alias\nitem.show()", want: "Record", wantCompletion: "Record"},
		{name: "CallResult", source: "getRecord().show()", want: "Record", wantCompletion: "Record"},
		{name: "ImportedReceiver", source: "var item Item\nitem.read(1)", want: "Item"},
		{name: "MissingReceiver", source: "missing.show()", invalid: true},
		{name: "UnnamedReceiver", source: "var item struct { Show func() }\nitem.Show()"},
		{name: "InvalidReceiver", source: "var item string\nitem.show()", invalid: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newClassfileTestServer(t, map[string][]byte{
				"First.first":      nil,
				"Worker.firstwork": []byte(tt.source + "\n"),
				"types.xgo":        []byte("type Record struct { Count int }\nfunc (r *Record) show() {}\nfunc getRecord() *Record { return nil }\n"),
			})
			proj := s.getProj()
			config := classfileTestModule()
			config.Opt.Projects[0].Works[0].Prefix = "Actor"
			proj.SetModule(newTestModule(t, config))
			info, err := proj.TypeInfo()
			if tt.invalid {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.NotNil(t, info)
			file, err := proj.ASTFile("Worker.firstwork")
			require.NoError(t, err)
			require.NotNil(t, file.ShadowEntry)
			require.NotEmpty(t, file.ShadowEntry.Body.List)
			stmt := requireValueAs[*ast.ExprStmt](t, file.ShadowEntry.Body.List[len(file.ShadowEntry.Body.List)-1])
			call := requireValueAs[*ast.CallExpr](t, stmt.X)
			named := propertyTargetForCall(proj, file, call)
			if tt.want == "" {
				assert.Nil(t, named)
			} else {
				require.NotNil(t, named)
				assert.Equal(t, tt.want, named.Obj().Name())
			}
			for _, enclosing := range []bool{false, true} {
				ctx := &completionContext{
					definitionContext: definitionContext{proj: proj}, astFile: file,
				}
				if enclosing {
					ctx.enclosingCallExpr = call
				} else {
					ctx.kind = completionKindCall
					ctx.enclosingNode = call
				}
				assert.Equal(t, tt.wantCompletion, ctx.getPropertyTarget())
			}
		})
	}
}
