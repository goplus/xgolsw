package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompileResultSpxResourceDocumentLinks(t *testing.T) {
	s := newTestServer(t, map[string][]byte{
		"main.xgo":                         []byte("var Runner int\nconst Scene = \"Studio\"\necho \"Studio\", \"Beep\", Runner, \"idle\", \"walk\", \"Score\", Scene\n"),
		"other.xgo":                        []byte("const Other = \"Studio\"\n"),
		"assets/index.json":                []byte(`{"backdrops":[{"name":"Studio"}],"zorder":[{"name":"Score"}]}`),
		"assets/sounds/Beep/index.json":    []byte(`{}`),
		"assets/sprites/Runner/index.json": []byte(`{"costumes":[{"name":"idle"}],"fAnimations":{"walk":{}}}`),
	})
	proj := s.getProj()
	call := spxResourceTestCall(t, proj, "main.xgo")
	require.Len(t, call.Args, 7)
	set, err := NewSpxResourceSet(proj)
	require.NoError(t, err)
	result := newCompileResult(proj, s.lookupPkgDoc)
	result.spxResourceSet = *set
	var want []DocumentLink
	for i, resource := range []struct {
		id         SpxResourceID
		kind       SpxResourceRefKind
		start, end uint32
		uri        URI
	}{
		{SpxBackdropResourceID{"Studio"}, SpxResourceRefKindStringLiteral, 5, 13, "spx://resources/backdrops/Studio"},
		{SpxSoundResourceID{"Beep"}, SpxResourceRefKindStringLiteral, 15, 21, "spx://resources/sounds/Beep"},
		{SpxSpriteResourceID{"Runner"}, SpxResourceRefKindAutoBindingReference, 23, 29, "spx://resources/sprites/Runner"},
		{SpxSpriteCostumeResourceID{"Runner", "idle"}, SpxResourceRefKindStringLiteral, 31, 37, "spx://resources/sprites/Runner/costumes/idle"},
		{SpxSpriteAnimationResourceID{"Runner", "walk"}, SpxResourceRefKindStringLiteral, 39, 45, "spx://resources/sprites/Runner/animations/walk"},
		{SpxWidgetResourceID{"Score"}, SpxResourceRefKindStringLiteral, 47, 54, "spx://resources/widgets/Score"},
		{SpxBackdropResourceID{"Studio"}, SpxResourceRefKindConstantReference, 56, 61, "spx://resources/backdrops/Studio"},
	} {
		ref := SpxResourceRef{ID: resource.id, Kind: resource.kind, Node: call.Args[i]}
		result.addSpxResourceRef(ref)
		result.addSpxResourceRef(ref)
		want = append(want, DocumentLink{
			Range:  Range{Start: Position{Line: 2, Character: resource.start}, End: Position{Line: 2, Character: resource.end}},
			Target: &resource.uri,
			Data:   SpxResourceRefDocumentLinkData{Kind: resource.kind},
		})
	}
	// Matching names in another resource kind or sprite do not make a resource exist.
	for _, id := range []SpxResourceID{
		SpxBackdropResourceID{"Beep"},
		SpxSoundResourceID{"Studio"},
		SpxSpriteResourceID{"Score"},
		SpxSpriteCostumeResourceID{"Runner", "walk"},
		SpxSpriteCostumeResourceID{"Missing", "idle"},
		SpxSpriteAnimationResourceID{"Runner", "idle"},
		SpxSpriteAnimationResourceID{"Missing", "walk"},
		SpxWidgetResourceID{"Runner"},
	} {
		result.addSpxResourceRef(SpxResourceRef{ID: id, Kind: SpxResourceRefKindStringLiteral, Node: call.Args[0]})
	}
	otherFile, err := proj.ASTFile("other.xgo")
	require.NoError(t, err)
	require.Len(t, otherFile.Decls, 1)
	decl := requireValueAs[*ast.GenDecl](t, otherFile.Decls[0])
	require.Len(t, decl.Specs, 1)
	spec := requireValueAs[*ast.ValueSpec](t, decl.Specs[0])
	require.Len(t, spec.Values, 1)
	result.addSpxResourceRef(SpxResourceRef{ID: SpxBackdropResourceID{"Studio"}, Kind: SpxResourceRefKindStringLiteral, Node: spec.Values[0]})

	assert.ElementsMatch(t, want, result.spxResourceDocumentLinks("main.xgo"))
	assert.Equal(t, []DocumentLink{{
		Range:  Range{Start: Position{Character: 14}, End: Position{Character: 22}},
		Target: toURI("spx://resources/backdrops/Studio"),
		Data:   SpxResourceRefDocumentLinkData{Kind: SpxResourceRefKindStringLiteral},
	}}, result.spxResourceDocumentLinks("other.xgo"))
	assert.Empty(t, result.spxResourceDocumentLinks("missing.xgo"))
	assert.ElementsMatch(t, want, result.spxResourceDocumentLinks("main.xgo"))
	assert.Empty(t, newCompileResult(proj, s.lookupPkgDoc).spxResourceDocumentLinks("main.xgo"))
}

func TestCompileResultSpxResourceHover(t *testing.T) {
	for _, tt := range []struct {
		name string
		id   SpxResourceID
		uri  string
	}{
		{"Backdrop", SpxBackdropResourceID{"Studio"}, "spx://resources/backdrops/Studio"},
		{"Sound", SpxSoundResourceID{"Beep"}, "spx://resources/sounds/Beep"},
		{"Sprite", SpxSpriteResourceID{"Runner"}, "spx://resources/sprites/Runner"},
		{"Costume", SpxSpriteCostumeResourceID{"Runner", "idle"}, "spx://resources/sprites/Runner/costumes/idle"},
		{"Animation", SpxSpriteAnimationResourceID{"Runner", "walk"}, "spx://resources/sprites/Runner/animations/walk"},
		{"Widget", SpxWidgetResourceID{"Score"}, "spx://resources/widgets/Score"},
		{"EscapedName", SpxSoundResourceID{"A/B ?#\""}, "spx://resources/sounds/A%2FB%20%3F%23%22"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, kind := range []MarkupKind{Markdown, PlainText} {
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte("echo \"resource\"\n")})
				proj := s.getProj()
				call := spxResourceTestCall(t, proj, "main.xgo")
				require.Len(t, call.Args, 1)
				result := newCompileResult(proj, s.lookupPkgDoc)
				position := token.Position{Filename: "main.xgo", Line: 1, Column: 8}
				assert.Nil(t, result.spxResourceHover(position, kind))
				result.addSpxResourceRef(SpxResourceRef{ID: tt.id, Kind: SpxResourceRefKindStringLiteral, Node: call.Args[0]})
				content := tt.uri
				if kind == Markdown {
					content = "<resource-preview resource=\"" + tt.uri + "\" />\n"
				}
				// A recorded reference has a preview even when the resource is missing.
				want := &Hover{
					Contents: MarkupContent{Kind: kind, Value: content},
					Range:    Range{Start: Position{Character: 5}, End: Position{Character: 15}},
				}
				assert.Equal(t, want, result.spxResourceHover(position, kind))
				position.Filename = "other.xgo"
				assert.Nil(t, result.spxResourceHover(position, kind))
				position = token.Position{Filename: "main.xgo", Line: 1, Column: 1}
				assert.Nil(t, result.spxResourceHover(position, kind))
			}
		})
	}
}

func TestSpxResourceRefSourceRanges(t *testing.T) {
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
			metadata, err := json.Marshal(map[string]any{"backdrops": []map[string]string{{"name": tt.value}}})
			require.NoError(t, err)
			s := newTestServer(t, map[string][]byte{
				"main.xgo":          []byte(tt.prefix + "echo \"\U0001f600\", " + tt.literal + "\n"),
				"assets/index.json": metadata,
			})
			proj := s.getProj()
			call := spxResourceTestCall(t, proj, "main.xgo")
			require.Len(t, call.Args, 2)
			node := call.Args[1]
			id := SpxBackdropResourceID{tt.value}
			result := newCompileResult(proj, s.lookupPkgDoc)
			set, err := NewSpxResourceSet(proj)
			require.NoError(t, err)
			result.spxResourceSet = *set
			result.addSpxResourceRef(SpxResourceRef{ID: id, Kind: SpxResourceRefKindStringLiteral, Node: node})
			wantRange := Range{Start: Position{Line: uint32(strings.Count(tt.prefix, "\n")), Character: 11}, End: tt.end}
			links := result.spxResourceDocumentLinks("main.xgo")
			require.Len(t, links, 1)
			assert.Equal(t, wantRange, links[0].Range)
			assert.Equal(t, toURI(string(id.URI())), links[0].Target)

			file, err := proj.ASTFile("main.xgo")
			require.NoError(t, err)
			// Hit the closing quote, including on a later line or after stripped CRs.
			position := tt.end
			position.Character--
			hover := result.spxResourceHover(ToPosition(proj, file, position), PlainText)
			require.NotNil(t, hover)
			assert.Equal(t, wantRange, hover.Range)
			assert.Equal(t, string(id.URI()), hover.Contents.Value)

			s.addSpxResourceNotFoundDiagnostic(result, node, "backdrop", tt.value, "")
			require.Len(t, result.diagnostics["file:///main.xgo"], 1)
			assert.Equal(t, wantRange, result.diagnostics["file:///main.xgo"][0].Range)
			changes := s.spxRenameResourceAtRefs(result, id, "Park")
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
					"main.xgo":          []byte(source),
					"other.xgo":         []byte("func preview() {\n\techo \"Studio\"\n}\n"),
					"assets/index.json": []byte(`{"backdrops":[{"name":"Studio"}]}`),
				})
				proj := s.getProj()
				call := spxResourceTestCall(t, proj, "main.xgo")
				require.Len(t, call.Args, 2)
				otherFile, err := proj.ASTFile("other.xgo")
				require.NoError(t, err)
				require.Len(t, otherFile.Decls, 1)
				otherFunc := requireValueAs[*ast.FuncDecl](t, otherFile.Decls[0])
				require.Len(t, otherFunc.Body.List, 1)
				otherStmt := requireValueAs[*ast.ExprStmt](t, otherFunc.Body.List[0])
				otherCall := requireValueAs[*ast.CallExpr](t, otherStmt.X)
				require.Len(t, otherCall.Args, 1)
				result := newCompileResult(proj, s.lookupPkgDoc)
				set, err := NewSpxResourceSet(proj)
				require.NoError(t, err)
				result.spxResourceSet = *set
				id := SpxBackdropResourceID{"Studio"}
				for _, node := range []ast.Expr{call.Args[0], otherCall.Args[0]} {
					result.addSpxResourceRef(SpxResourceRef{ID: id, Kind: SpxResourceRefKindStringLiteral, Node: node})
				}
				position := proj.Fset.Position(call.Args[0].Pos())
				require.Len(t, result.spxResourceDocumentLinks("main.xgo"), 1)
				require.NotNil(t, result.spxResourceHover(position, PlainText))
				ref, file := result.spxResourceRefAtPosition(position)
				require.NotNil(t, ref)
				require.NotNil(t, file)
				if tt.content == nil {
					require.NoError(t, proj.DeleteFile("main.xgo"))
				} else {
					s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: tt.content, Version: 1}})
				}

				assert.Empty(t, result.spxResourceDocumentLinks("main.xgo"))
				assert.Nil(t, result.spxResourceHover(position, PlainText))
				// A resolved reference retains the source used to calculate its range.
				assert.Equal(t, Range{Start: Position{Character: 5}, End: Position{Character: 14}}, resourceRange(proj, file, ref.Node))
				s.addSpxResourceNotFoundDiagnostic(result, call.Args[0], "backdrop", "Studio", "")
				s.addEmptySpxResourceNameDiagnostic(result, call.Args[1], "backdrop")
				assert.Empty(t, result.diagnostics)
				assert.Equal(t, map[DocumentURI][]TextEdit{
					"file:///other.xgo": {{Range: Range{Start: Position{Line: 1, Character: 7}, End: Position{Line: 1, Character: 13}}, NewText: "Park"}},
				}, s.spxRenameResourceAtRefs(result, id, "Park"))
				assert.Len(t, result.spxResourceDocumentLinks("other.xgo"), 1)
				assert.NotNil(t, result.spxResourceHover(proj.Fset.Position(otherCall.Args[0].Pos()), PlainText))
			})
		}
	})
}
