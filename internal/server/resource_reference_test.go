package server

import (
	gotypes "go/types"
	"slices"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testResourceResolver(t *testing.T, proj *xgo.Project) func(resourceValue) (resourceID, bool) {
	t.Helper()

	info, _ := proj.TypeInfo()
	require.NotNil(t, info)
	asset := info.Pkg.Scope().Lookup("Asset")
	require.NotNil(t, asset)
	assetType := asset.Type()
	return func(value resourceValue) (resourceID, bool) {
		if gotypes.Unalias(value.Type) != assetType {
			return nil, false
		}
		return testResourceID{"files", value.Name}, true
	}
}

func TestResourceReferences(t *testing.T) {
	for _, tt := range []struct {
		name   string
		source string
		want   []string
	}{
		{"Declaration", "var asset Asset = \"declaration\"\n", []string{"declaration"}},
		{"Assignment", "var asset Asset\nasset = \"assignment\"\n", []string{"assignment"}},
		{"Return", "func current() Asset { return \"return\" }\nprintln current()\n", []string{"return"}},
		{"FunctionVariable", "var consume = func(asset Asset) {}\nconsume \"value\"\n", []string{"value"}},
		{"NamedFunction", "type Consumer func(asset Asset)\nvar consume Consumer\nconsume \"value\"\n", []string{"value"}},
		{"FunctionField", "type Handler struct { Consume func(asset Asset) }\nvar handler Handler\nhandler.Consume \"value\"\n", []string{"value"}},
		{"FunctionResult", "func consumer() func(asset Asset) { return nil }\nconsumer()(\"value\")\n", []string{"value"}},
		{"ParenthesizedFunction", "(use)(\"value\")\n", []string{"value"}},
		{"MethodExpression", "type Handler struct{}\nvar handler Handler\nfunc (h Handler) Consume(asset Asset) {}\nHandler.Consume(handler, \"value\")\n", []string{"value"}},
		{"Tuple", "func consume(count int, asset Asset) {}\nconsume((1, \"value\"))\n", []string{"value"}},
		{"TupleFunctionValue", "var consume = func(count int, asset Asset) {}\nconsume((1, \"value\"))\n", []string{"value"}},
		{"TupleValue", "func consume(pair (int, Asset)) {}\nconsume((1, \"value\"))\n", []string{"value"}},
		{"NestedTupleValue", "type Pair (int, Asset)\nfunc consume(pair (string, Pair)) {}\nconsume((\"ignored\", (1, Asset(\"value\"))))\n", []string{"value"}},
		{"TupleDeclaration", "var pair (string, Asset) = (\"ignored\", Asset(\"value\"))\n", []string{"value"}},
		{"TupleReturn", "type Pair (string, Asset)\nfunc pair() Pair { return (\"ignored\", Asset(\"value\")) }\n", []string{"value"}},
		{"LambdaReturn", "func consume(callback func() Asset) {}\nconsume(() => { return \"value\" })\n", []string{"value"}},
		{"ArrowResult", "func consume(callback func() Asset) {}\nconsume(() => \"value\")\n", []string{"value"}},
		{"NestedLambdaReturn", "func consume(callback func() func() Asset) {}\nconsume(() => { return () => { return \"value\" } })\n", []string{"value"}},
		{"LambdaAssignment", "var callback func() Asset = () => { return \"value\" }\n", []string{"value"}},
		{"LambdaBoundary", "func consume(callback func() Asset) {}\nconsume(() => { func() string { return \"ignored\" }(); return \"value\" })\n", []string{"value"}},
		{"TupleVariadic", "func consume(assets ...Asset) {}\nconsume((\"first\", \"second\"))\n", []string{"first", "second"}},
		{"Goto", "func Goto(asset Asset) {}\nconst target = \"value\"\ngoto target\n", []string{"value"}},
		{"Call", "use \"call\"\n", []string{"call"}},
		{"Kwarg", "type Options struct { Asset Asset }\nfunc option(opts Options?) {}\noption asset = \"kwarg\"\n", []string{"kwarg"}},
		{"Constant", "const asset = \"constant\"\nuse asset\n", []string{"constant"}},
		{"ConstantExpression", "const asset Asset = \"prefix\" + \"suffix\"\nuse asset\n", []string{"prefixsuffix"}},
		{"Parenthesized", "use (\"parenthesized\")\n", []string{"parenthesized"}},
		{"Conversion", "println Asset(\"intrinsic\")\n", []string{"intrinsic"}},
		{"NestedConversion", "println Asset(string(\"intrinsic\"))\n", []string{"intrinsic"}},
		{"ConversionArgument", "use Asset(\"argument\")\n", []string{"argument"}},
		{"ConversionInitializer", "const asset = Asset(\"initializer\")\nuse asset\n", []string{"initializer", "initializer"}},
		{"NumericConversion", "println Asset(65)\n", nil},
		{"ConstantConversion", "const name = \"constant\"\nprintln Asset(name)\n", nil},
		{"UnrelatedArgument", "func fromText(text string) Asset { return \"result\" }\nasset := fromText(\"unrelated\")\n", []string{"result"}},
		{"IncompleteCall", "use \"partial\", missing\n", []string{"partial"}},
		{"Variadic", "func many(assets ...Asset) {}\nmany \"first\", \"second\"\n", []string{"first", "second"}},
		{"Slice", "func many(assets []Asset) {}\nmany [\"first\", (\"second\")]\n", []string{"first", "second"}},
		{"VariadicSlice", "func many(assets ...Asset) {}\nmany [\"first\", \"second\"]...\n", []string{"first", "second"}},
		{"KwargSlice", "type Options struct { Assets []Asset }\nfunc option(opts Options?) {}\noption assets = ([\"first\", \"second\"])\n", []string{"first", "second"}},
		{"OverloadKwarg", "type Worker struct{}\ntype Options struct { Assets []Asset }\nvar worker Worker\nfunc (w *Worker) useAssets(opts Options?) {}\nfunc (Worker).use = ((Worker).useAssets)\nworker.use assets = [\"first\", \"second\"]\n", []string{"first", "second"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, factory := range []struct {
				name     string
				filename string
				new      testServerFactory
			}{
				{"Plain", "main.xgo", newTestServer},
				{"Classfile", "main_fixture.gox", newFrameworkTestServer},
			} {
				t.Run(factory.name, func(t *testing.T) {
					s := factory.new(t, map[string][]byte{factory.filename: []byte(tt.source), "resources.xgo": []byte("type Asset string\nfunc use(asset Asset) {}\n")})
					proj := s.getProj()
					info, err := proj.TypeInfo()
					require.NotNil(t, info)
					if tt.name != "IncompleteCall" {
						require.NoError(t, err)
					}
					result := newTestResourceAnalysis()
					for ref := range resourceReferences(proj, testResourceResolver(t, proj)) {
						result.addResourceRef(ref)
					}
					var names []string
					for _, ref := range result.resourceRefs {
						names = append(names, ref.ID.Name())
						assert.NotNil(t, sourceASTFile(proj, ref.Node.Pos()))
						if tt.name == "Constant" {
							assert.Equal(t, XGoResourceRefKindConstantReference, ref.Kind)
						}
						if _, converted := ref.Node.(*ast.CallExpr); converted {
							hover := result.resourceHover(s.getProj(), proj.Fset.PositionFor(ref.Node.Pos(), false), Markdown)
							assert.Nil(t, hover, "the conversion type must retain its symbol hover")
							file := sourceASTFile(proj, ref.Node.Pos())
							span := resourceRange(proj, file, ref.Node)
							text := tt.source[PositionOffset([]byte(tt.source), span.Start):PositionOffset([]byte(tt.source), span.End)]
							assert.Equal(t, `"`+ref.ID.Name()+`"`, text)
						}
					}
					assert.ElementsMatch(t, tt.want, names)
				})
			}
		})
	}

	t.Run("ContextTakesPrecedence", func(t *testing.T) {
		for _, available := range []bool{false, true} {
			s := newFrameworkTestServer(t, map[string][]byte{"main_fixture.gox": []byte("type Asset string\nconst asset Asset = \"item\"\nfunc use(value Asset) {}\nuse asset\n")})
			proj := s.getProj()
			resolve := testResourceResolver(t, proj)
			call := resourceTestCall(t, proj, "main_fixture.gox")
			var callRefs []resourceRef
			var visitedIntrinsic bool
			for ref := range resourceReferences(proj, func(value resourceValue) (resourceID, bool) {
				if value.Expr == call.Args[0] {
					visitedIntrinsic = visitedIntrinsic || value.Intrinsic
					if !available {
						return nil, true
					}
					return testResourceID{"scoped", value.Name}, true
				}
				return resolve(value)
			}) {
				if ref.Node == call.Args[0] {
					callRefs = append(callRefs, ref)
				}
			}
			assert.False(t, visitedIntrinsic)
			if available {
				require.Len(t, callRefs, 1)
				assert.Equal(t, testResourceID{"scoped", "item"}, callRefs[0].ID)
			} else {
				assert.Empty(t, callRefs)
			}
		}
	})

	t.Run("StopIteration", func(t *testing.T) {
		for _, tt := range []struct {
			name          string
			source        string
			intrinsicOnly bool
		}{
			{"Declarations", "var a Asset = \"first\"\nvar b Asset = \"second\"\n", false},
			{"Builtin", "var values []Asset\nvalues = append(values, \"first\", \"second\")\n", false},
			{"Send", "var values chan Asset\nvalues <- \"first\"\nvalues <- \"second\"\n", false},
			{"Index", "var values map[Asset]int\necho values[\"first\"], values[\"second\"]\n", false},
			{"Comparison", "var value Asset\necho value == \"first\", value != \"second\"\n", false},
			{"Switch", "var value Asset\nswitch value {case \"first\", \"second\":}\n", false},
			{"Min", "var value Asset\necho min(value, \"first\", \"second\")\n", false},
			{"Calls", "use \"first\"\nuse \"second\"\n", false},
			{"Intrinsic", "const a Asset = \"first\"\nconst b Asset = \"second\"\nprintln a, b\n", true},
			{"Conversion", "println Asset(\"first\"), Asset(\"second\")\n", true},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte("type Asset string\nfunc use(asset Asset) {}\n" + tt.source)})
				proj := s.getProj()
				resolve := testResourceResolver(t, proj)
				var refs []resourceRef
				for ref := range resourceReferences(proj, func(value resourceValue) (resourceID, bool) {
					if tt.intrinsicOnly && !value.Intrinsic {
						return nil, false
					}
					return resolve(value)
				}) {
					refs = append(refs, ref)
					break
				}
				require.Len(t, refs, 1)
			})
		}
	})

	t.Run("NoTypeInfo", func(t *testing.T) {
		s := newTestServer(t, nil)
		refs := slices.Collect(resourceReferences(s.getProj(), func(resourceValue) (resourceID, bool) {
			assert.Fail(t, "unexpected resource resolution")
			return nil, false
		}))
		assert.Empty(t, refs)
	})
}

func TestResourceAnalysisUnavailableMetadata(t *testing.T) {
	s := newTestServer(t, map[string][]byte{"main.xgo": []byte("echo \"missing\"\n")})
	proj := s.getProj()
	call := resourceTestCall(t, proj, "main.xgo")
	lit := requireValueAs[*ast.BasicLit](t, call.Args[0])
	result := &resourceAnalysis{}
	result.addResourceRef(resourceRef{ID: testResourceID{"files", "missing"}, Node: lit})
	assert.Empty(t, result.resourceDocumentLinks(s.getProj(), "main.xgo"))
	assert.NotNil(t, result.resourceHover(s.getProj(), proj.Fset.Position(lit.Pos()), Markdown))
}

func TestResourceReferencesStringLiterals(t *testing.T) {
	for _, tt := range []struct {
		name    string
		literal string
		want    string
		static  bool
	}{
		{"Dollar", `"$$"`, "$", true},
		{"RepeatedDollars", `"$$$$"`, "$$", true},
		{"DollarInText", `"A$$B"`, "A$B", true},
		{"EscapedInterpolation", `"$${name}"`, "${name}", true},
		{"HexDollars", `"\x24\x24"`, "$$", true},
		{"HexInterpolation", `"\x24{name}"`, "${name}", true},
		{"MixedEscapes", `"\x24$$"`, "$$", true},
		{"QuotesAndDollars", `"A\"$$B"`, "A\"$B", true},
		{"RawDollar", "`$$`", "$", true},
		{"RawCarriageReturns", "`A\r$$\rB`", "A$B", true},
		{"Interpolation", `"${name}"`, "", false},
		{"InterpolationInText", `"icon-${name}"`, "", false},
		{"RawInterpolation", "`${name}`", "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte("var name = \"dynamic\"\nfunc use(value string) {}\nuse " + tt.literal + "\n")})
			proj := s.getProj()
			call := resourceTestCall(t, proj, "main.xgo")
			refs := slices.Collect(resourceReferences(proj, func(value resourceValue) (resourceID, bool) {
				if value.Call != call {
					return nil, false
				}
				return testResourceID{"files", value.Name}, true
			}))
			if !tt.static {
				assert.Empty(t, refs)
				return
			}
			require.Len(t, refs, 1)
			assert.Equal(t, tt.want, refs[0].ID.Name())
			assert.Same(t, call.Args[0], refs[0].Node)
		})
	}
}
