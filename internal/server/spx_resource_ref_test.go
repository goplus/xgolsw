package server

import (
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompileResultAddSpxResourceRef(t *testing.T) {
	s := newTestServer(t, map[string][]byte{"main.xgo": []byte("echo \"Studio\", \"Studio\"\n")})
	proj := s.getProj()
	call := spxResourceTestCall(t, proj, "main.xgo")
	require.Len(t, call.Args, 2)
	result := newCompileResult(proj, s.lookupPkgDoc)
	first := SpxResourceRef{ID: SpxBackdropResourceID{BackdropName: "Studio"}, Kind: SpxResourceRefKindStringLiteral, Node: call.Args[0]}
	second := first
	second.Node = call.Args[1]
	otherID := first
	otherID.ID = SpxSoundResourceID{SoundName: "Studio"}
	otherKind := first
	otherKind.Kind = SpxResourceRefKindConstantReference
	for _, ref := range []SpxResourceRef{first, first, second, otherID, otherKind, second} {
		result.addSpxResourceRef(ref)
	}
	assert.Equal(t, []SpxResourceRef{first, second, otherID, otherKind}, result.spxResourceRefs)
	other := newCompileResult(proj, s.lookupPkgDoc)
	other.addSpxResourceRef(first)
	assert.Equal(t, []SpxResourceRef{first}, other.spxResourceRefs)
}

func TestCompileResultSpxResourceRefAtPosition(t *testing.T) {
	for _, tt := range []struct {
		name     string
		filename string
		line     int
		column   int
		want     int
	}{
		{name: "SmallestReference", filename: "main.xgo", line: 1, column: 9, want: 1},
		{name: "Start", filename: "main.xgo", line: 1, column: 7, want: 1},
		{name: "End", filename: "main.xgo", line: 1, column: 15, want: 1},
		{name: "OuterReference", filename: "main.xgo", line: 1, column: 6, want: 0},
		{name: "Sibling", filename: "main.xgo", line: 1, column: 20, want: 2},
		{name: "Before", filename: "main.xgo", line: 1, column: 4, want: -1},
		{name: "After", filename: "main.xgo", line: 1, column: 25, want: -1},
		{name: "OtherLine", filename: "main.xgo", line: 2, column: 9, want: -1},
		{name: "OtherFile", filename: "other.xgo", line: 1, column: 9, want: -1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte("echo (\"Studio\"), \"Beep\"\n")})
			proj := s.getProj()
			call := spxResourceTestCall(t, proj, "main.xgo")
			require.Len(t, call.Args, 2)
			paren := requireValueAs[*ast.ParenExpr](t, call.Args[0])
			refs := []SpxResourceRef{
				{ID: SpxSpriteResourceID{SpriteName: "Runner"}, Node: paren},
				{ID: SpxBackdropResourceID{BackdropName: "Studio"}, Node: paren.X},
				{ID: SpxSoundResourceID{SoundName: "Beep"}, Node: call.Args[1]},
			}
			for _, orderedRefs := range [][]SpxResourceRef{refs, {refs[2], refs[1], refs[0]}} {
				result := newCompileResult(proj, s.lookupPkgDoc)
				result.spxResourceRefs = orderedRefs
				ref, _ := result.spxResourceRefAtPosition(token.Position{Filename: tt.filename, Line: tt.line, Column: tt.column})
				if tt.want < 0 {
					assert.Nil(t, ref)
				} else {
					assert.Equal(t, &refs[tt.want], ref)
				}
			}
		})
	}

	t.Run("SourceRanges", func(t *testing.T) {
		type positionCheck struct {
			position Position
			want     int
		}
		for _, tt := range []struct {
			name   string
			source string
			checks []positionCheck
		}{
			{
				name: "Multiline", source: "echo (\n    `Long first line\nx\nend`), \"Beep\"\n",
				checks: []positionCheck{
					{Position{Line: 0, Character: 4}, -1},
					{Position{Line: 0, Character: 5}, 0},
					{Position{Line: 1, Character: 1}, 0},
					{Position{Line: 1, Character: 4}, 1},
					{Position{Line: 1, Character: 17}, 1},
					{Position{Line: 2, Character: 0}, 1},
					{Position{Line: 3, Character: 3}, 1},
					{Position{Line: 3, Character: 4}, 1},
					{Position{Line: 3, Character: 5}, 0},
					{Position{Line: 3, Character: 6}, -1},
					{Position{Line: 3, Character: 8}, 2},
				},
			},
			{
				name: "CarriageReturns", source: "echo (`S\rtu\r\rdio\r`), \"Beep\"\n",
				checks: []positionCheck{
					{Position{Character: 15}, 1},
					{Position{Character: 17}, 1},
					{Position{Character: 18}, 1},
					{Position{Character: 19}, 0},
					{Position{Character: 20}, -1},
				},
			},
			{
				name: "CRLFAndUnicode", source: "echo (\r\n    `\U0001f600\r\nx\r\n\U0001f600`), \"Beep\"\r\n",
				checks: []positionCheck{
					{Position{Line: 1, Character: 5}, 1},
					{Position{Line: 2, Character: 0}, 1},
					{Position{Line: 3, Character: 2}, 1},
					{Position{Line: 3, Character: 3}, 1},
					{Position{Line: 3, Character: 4}, 0},
					{Position{Line: 3, Character: 5}, -1},
					{Position{Line: 3, Character: 7}, 2},
				},
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte(tt.source)})
				proj := s.getProj()
				call := spxResourceTestCall(t, proj, "main.xgo")
				require.Len(t, call.Args, 2)
				paren := requireValueAs[*ast.ParenExpr](t, call.Args[0])
				refs := []SpxResourceRef{
					{ID: SpxSpriteResourceID{SpriteName: "Runner"}, Node: paren},
					{ID: SpxBackdropResourceID{BackdropName: "Studio"}, Node: paren.X},
					{ID: SpxSoundResourceID{SoundName: "Beep"}, Node: call.Args[1]},
				}
				file, err := proj.ASTFile("main.xgo")
				require.NoError(t, err)
				for _, order := range [][]SpxResourceRef{refs, {refs[2], refs[1], refs[0]}} {
					result := newCompileResult(proj, s.lookupPkgDoc)
					result.spxResourceRefs = order
					for _, check := range tt.checks {
						ref, _ := result.spxResourceRefAtPosition(ToPosition(proj, file, check.position))
						if check.want < 0 {
							assert.Nil(t, ref, "at %v", check.position)
						} else {
							assert.Equal(t, &refs[check.want], ref, "at %v", check.position)
						}
					}
				}
			})
		}
	})
}

func spxResourceTestCall(t *testing.T, proj *xgo.Project, filename string) *ast.CallExpr {
	t.Helper()

	file, err := proj.ASTFile(filename)
	require.NoError(t, err)
	_, err = proj.TypeInfo()
	require.NoError(t, err)
	require.NotNil(t, file.ShadowEntry)
	require.Len(t, file.ShadowEntry.Body.List, 1)
	stmt := requireValueAs[*ast.ExprStmt](t, file.ShadowEntry.Body.List[0])
	return requireValueAs[*ast.CallExpr](t, stmt.X)
}
