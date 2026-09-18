package server

import (
	"strings"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResourceAnalysisResourceDocumentLinks(t *testing.T) {
	s := newTestServer(t, map[string][]byte{
		"main.xgo":  []byte("var Runner int\nconst Scene = \"Studio\"\necho \"Studio\", \"Beep\", Runner, \"idle\", \"walk\", \"Score\", Scene\n"),
		"other.xgo": []byte("const Other = \"Studio\"\n"),
	})
	proj := s.getProj()
	call := resourceTestCall(t, proj, "main.xgo")
	require.Len(t, call.Args, 7)
	result := newTestResourceAnalysis(proj,
		testResourceID{"scenes", "Studio"}, testResourceID{"clips", "Beep"},
		testResourceID{"actors", "Runner"}, testResourceID{"actors/Runner/skins", "idle"},
		testResourceID{"actors/Runner/sequences", "walk"}, testResourceID{"controls", "Score"},
	)
	var want []DocumentLink
	for i, resource := range []struct {
		id         resourceID
		kind       XGoResourceRefKind
		start, end uint32
		uri        URI
	}{
		{testResourceID{"scenes", "Studio"}, XGoResourceRefKindStringLiteral, 5, 13, "test://resources/scenes/Studio"},
		{testResourceID{"clips", "Beep"}, XGoResourceRefKindStringLiteral, 15, 21, "test://resources/clips/Beep"},
		{testResourceID{"actors", "Runner"}, XGoResourceRefKindAutoBindingReference, 23, 29, "test://resources/actors/Runner"},
		{testResourceID{"actors/Runner/skins", "idle"}, XGoResourceRefKindStringLiteral, 31, 37, "test://resources/actors/Runner/skins/idle"},
		{testResourceID{"actors/Runner/sequences", "walk"}, XGoResourceRefKindStringLiteral, 39, 45, "test://resources/actors/Runner/sequences/walk"},
		{testResourceID{"controls", "Score"}, XGoResourceRefKindStringLiteral, 47, 54, "test://resources/controls/Score"},
		{testResourceID{"scenes", "Studio"}, XGoResourceRefKindConstantReference, 56, 61, "test://resources/scenes/Studio"},
	} {
		ref := resourceRef{ID: resource.id, Kind: resource.kind, Node: call.Args[i]}
		result.addResourceRef(ref)
		result.addResourceRef(ref)
		want = append(want, DocumentLink{
			Range:  Range{Start: Position{Line: 2, Character: resource.start}, End: Position{Line: 2, Character: resource.end}},
			Target: &resource.uri,
			Data:   XGoResourceRefDocumentLinkData{Kind: resource.kind},
		})
	}
	// Matching names in another resource kind or sprite do not make a resource exist.
	for _, id := range []resourceID{
		testResourceID{"scenes", "Beep"},
		testResourceID{"clips", "Studio"},
		testResourceID{"actors", "Score"},
		testResourceID{"actors/Runner/skins", "walk"},
		testResourceID{"actors/Missing/skins", "idle"},
		testResourceID{"actors/Runner/sequences", "idle"},
		testResourceID{"actors/Missing/sequences", "walk"},
		testResourceID{"controls", "Runner"},
	} {
		result.addResourceRef(resourceRef{ID: id, Kind: XGoResourceRefKindStringLiteral, Node: call.Args[0]})
	}
	otherFile, err := proj.ASTFile("other.xgo")
	require.NoError(t, err)
	require.Len(t, otherFile.Decls, 1)
	decl := requireValueAs[*ast.GenDecl](t, otherFile.Decls[0])
	require.Len(t, decl.Specs, 1)
	spec := requireValueAs[*ast.ValueSpec](t, decl.Specs[0])
	require.Len(t, spec.Values, 1)
	result.addResourceRef(resourceRef{ID: testResourceID{"scenes", "Studio"}, Kind: XGoResourceRefKindStringLiteral, Node: spec.Values[0]})

	assert.ElementsMatch(t, want, result.resourceDocumentLinks("main.xgo"))
	assert.Equal(t, []DocumentLink{{
		Range:  Range{Start: Position{Character: 14}, End: Position{Character: 22}},
		Target: toURI("test://resources/scenes/Studio"),
		Data:   XGoResourceRefDocumentLinkData{Kind: XGoResourceRefKindStringLiteral},
	}}, result.resourceDocumentLinks("other.xgo"))
	assert.Empty(t, result.resourceDocumentLinks("missing.xgo"))
	assert.ElementsMatch(t, want, result.resourceDocumentLinks("main.xgo"))
	assert.Empty(t, newTestResourceAnalysis(proj).resourceDocumentLinks("main.xgo"))
}

func TestResourceAnalysisResourceHover(t *testing.T) {
	for _, tt := range []struct {
		name string
		id   resourceID
		uri  string
	}{
		{"Scene", testResourceID{"scenes", "Studio"}, "test://resources/scenes/Studio"},
		{"Clip", testResourceID{"clips", "Beep"}, "test://resources/clips/Beep"},
		{"Actor", testResourceID{"actors", "Runner"}, "test://resources/actors/Runner"},
		{"Skin", testResourceID{"actors/Runner/skins", "idle"}, "test://resources/actors/Runner/skins/idle"},
		{"Sequence", testResourceID{"actors/Runner/sequences", "walk"}, "test://resources/actors/Runner/sequences/walk"},
		{"Control", testResourceID{"controls", "Score"}, "test://resources/controls/Score"},
		{"EscapedName", testResourceID{"clips", "A/B ?#\""}, "test://resources/clips/A%2FB%20%3F%23%22"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, kind := range []MarkupKind{Markdown, PlainText} {
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte("echo \"resource\"\n")})
				proj := s.getProj()
				call := resourceTestCall(t, proj, "main.xgo")
				require.Len(t, call.Args, 1)
				result := newTestResourceAnalysis(proj)
				position := token.Position{Filename: "main.xgo", Line: 1, Column: 8}
				assert.Nil(t, result.resourceHover(position, kind))
				result.addResourceRef(resourceRef{ID: tt.id, Kind: XGoResourceRefKindStringLiteral, Node: call.Args[0]})
				content := tt.uri
				if kind == Markdown {
					content = "<resource-preview resource=\"" + tt.uri + "\" />\n"
				}
				// A recorded reference has a preview even when the resource is missing.
				want := &Hover{
					Contents: MarkupContent{Kind: kind, Value: content},
					Range:    Range{Start: Position{Character: 5}, End: Position{Character: 15}},
				}
				assert.Equal(t, want, result.resourceHover(position, kind))
				position.Filename = "other.xgo"
				assert.Nil(t, result.resourceHover(position, kind))
				position = token.Position{Filename: "main.xgo", Line: 1, Column: 1}
				assert.Nil(t, result.resourceHover(position, kind))
			}
		})
	}
}

func TestResourceRefSourceRanges(t *testing.T) {
	for _, tt := range []struct {
		name    string
		literal string
		value   string
		end     Position
		prefix  string
	}{
		{"Quoted", `"Studio"`, "Studio", Position{Character: 19}, ""},
		{"Escaped", `"Stu\x64io"`, "Studio", Position{Character: 22}, ""},
		{"Raw", "`Studio`", "Studio", Position{Character: 19}, ""},
		{"CarriageReturn", "`Stu\rdio`", "Studio", Position{Character: 20}, ""},
		{"MultipleCarriageReturns", "`S\rtu\r\rdio\r`", "Studio", Position{Character: 23}, ""},
		{"Multiline", "`Long first line\nx`", "Long first line\nx", Position{Line: 1, Character: 2}, ""},
		{"CRLFAndUnicode", "`Stu\r\n\r\n\U0001f600dio`", "Stu\n\n\U0001f600dio", Position{Line: 2, Character: 6}, ""},
		{"LineDirective", "`Stu\rdio`", "Studio", Position{Line: 1, Character: 20}, "//line virtual.xgo:100\n"},
		{"ColumnDirective", "`Stu\rdio`", "Studio", Position{Line: 1, Character: 20}, "//line virtual.xgo:100:20\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{
				"main.xgo": []byte(tt.prefix + "echo \"\U0001f600\", " + tt.literal + "\n"),
			})
			proj := s.getProj()
			call := resourceTestCall(t, proj, "main.xgo")
			require.Len(t, call.Args, 2)
			node := call.Args[1]
			id := testResourceID{"scenes", tt.value}
			result := newTestResourceAnalysis(proj, id)
			result.addResourceRef(resourceRef{ID: id, Kind: XGoResourceRefKindStringLiteral, Node: node})
			wantRange := Range{Start: Position{Line: uint32(strings.Count(tt.prefix, "\n")), Character: 11}, End: tt.end}
			links := result.resourceDocumentLinks("main.xgo")
			require.Len(t, links, 1)
			assert.Equal(t, wantRange, links[0].Range)
			assert.Equal(t, toURI(string(id.URI())), links[0].Target)

			file, err := proj.ASTFile("main.xgo")
			require.NoError(t, err)
			// Hit the closing quote, including on a later line or after stripped CRs.
			position := tt.end
			position.Character--
			hover := result.resourceHover(ToPosition(proj, file, position), PlainText)
			require.NotNil(t, hover)
			assert.Equal(t, wantRange, hover.Range)
			assert.Equal(t, string(id.URI()), hover.Contents.Value)

			s.addResourceDiagnostic(result, node, "resource not found")
			require.Len(t, result.diagnostics["file:///main.xgo"], 1)
			assert.Equal(t, wantRange, result.diagnostics["file:///main.xgo"][0].Range)
			changes := s.renameResourceAtRefs(t, result, id, "Park")
			require.Len(t, changes["file:///main.xgo"], 1)
			edit := changes["file:///main.xgo"][0]
			content := file.Code
			updated := string(content[:PositionOffset(content, edit.Range.Start)]) + edit.NewText + string(content[PositionOffset(content, edit.Range.End):])
			quote := tt.literal[:1]
			assert.Equal(t, tt.prefix+"echo \"\U0001f600\", "+quote+"Park"+quote+"\n", updated)
		})
	}

	t.Run("SourceChanges", func(t *testing.T) {
		const source = "echo `Stu\rdio`, ``\n"
		for _, tt := range []struct {
			name    string
			content []byte
		}{
			{"Deleted", nil},
			{"Shortened", []byte("\n")},
			{"Replaced", []byte("echo `A different value`\n")},
			{"Reparsed", []byte(source)},
			{"Incomplete", []byte("echo `")},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{
					"main.xgo":  []byte(source),
					"other.xgo": []byte("func preview() {\n\techo \"Studio\"\n}\n"),
				})
				proj := s.getProj()
				call := resourceTestCall(t, proj, "main.xgo")
				require.Len(t, call.Args, 2)
				otherFile, err := proj.ASTFile("other.xgo")
				require.NoError(t, err)
				require.Len(t, otherFile.Decls, 1)
				otherFunc := requireValueAs[*ast.FuncDecl](t, otherFile.Decls[0])
				require.Len(t, otherFunc.Body.List, 1)
				otherStmt := requireValueAs[*ast.ExprStmt](t, otherFunc.Body.List[0])
				otherCall := requireValueAs[*ast.CallExpr](t, otherStmt.X)
				require.Len(t, otherCall.Args, 1)
				result := newTestResourceAnalysis(proj, testResourceID{"scenes", "Studio"})
				id := testResourceID{"scenes", "Studio"}
				for _, node := range []ast.Expr{call.Args[0], otherCall.Args[0]} {
					result.addResourceRef(resourceRef{ID: id, Kind: XGoResourceRefKindStringLiteral, Node: node})
				}
				position := proj.Fset.Position(call.Args[0].Pos())
				require.Len(t, result.resourceDocumentLinks("main.xgo"), 1)
				require.NotNil(t, result.resourceHover(position, PlainText))
				ref, file := result.resourceRefAtPosition(position)
				require.NotNil(t, ref)
				require.NotNil(t, file)
				if tt.content == nil {
					require.NoError(t, proj.DeleteFile("main.xgo"))
				} else {
					s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: tt.content, Version: 1}})
				}

				assert.Empty(t, result.resourceDocumentLinks("main.xgo"))
				assert.Nil(t, result.resourceHover(position, PlainText))
				// A resolved reference retains the source used to calculate its range.
				assert.Equal(t, Range{Start: Position{Character: 5}, End: Position{Character: 14}}, resourceRange(proj, file, ref.Node))
				s.addResourceDiagnostic(result, call.Args[0], "resource not found")
				s.addResourceDiagnostic(result, call.Args[1], "resource name cannot be empty")
				assert.Empty(t, result.diagnostics)
				assert.Equal(t, map[DocumentURI][]TextEdit{
					"file:///other.xgo": {{Range: Range{Start: Position{Line: 1, Character: 7}, End: Position{Line: 1, Character: 13}}, NewText: "Park"}},
				}, s.renameResourceAtRefs(t, result, id, "Park"))
				assert.Len(t, result.resourceDocumentLinks("other.xgo"), 1)
				assert.NotNil(t, result.resourceHover(proj.Fset.Position(otherCall.Args[0].Pos()), PlainText))
			})
		}
	})
}
