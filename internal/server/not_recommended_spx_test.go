//go:build !test_no_pkgdata

package server

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpxNotRecommendedCourseHooks(t *testing.T) {
	const wrappers = `//ANNOTATION
func reportLitaTurn(angle float64) { println angle }
//ANNOTATION
func (p *Lita) Turn__0(angle Direction) {
    reportLitaTurn angle
    p.SpriteImpl.Turn__0(angle)
}
//ANNOTATION
func (p *Lita) Turn__1(angle Direction, speed float64) {
    reportLitaTurn angle
    p.SpriteImpl.Turn__1(angle, speed)
}
//ANNOTATION
func (p *Lita) Turn__2(angle Direction, speed float64, animation SpriteAnimationName) {
    reportLitaTurn angle
    p.SpriteImpl.Turn__2(angle, speed, animation)
}
//ANNOTATION
func (p *Lita) TurnWith(angle Direction, opts *MotionOptions?) {
    reportLitaTurn angle
    p.SpriteImpl.TurnWith(angle, opts)
}
`
	for _, tt := range []struct {
		name, main string
		wantHooks  bool
	}{
		{name: "Native"},
		{name: "Hooks", main: strings.ReplaceAll(wrappers, "//ANNOTATION", ""), wantHooks: true},
		{name: "AnnotatedHooks", main: strings.ReplaceAll(wrappers, "//ANNOTATION", "//xgols:notRecommended")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, "onStart => {\n|\nturn 90\nturnWith 90\n}\n")
			s := newSpxIntegrationTestServer(t, map[string][]byte{
				"main.spx": []byte(tt.main), "Lita.spx": []byte(source),
				"assets/index.json": []byte(`{}`), "assets/sprites/Lita/index.json": []byte(`{}`),
			})
			t.Cleanup(s.Close)
			requireNoDiagnostics(t, s)
			items := completionItemsAt(t, s, "Lita.spx", pos)
			var nativeTurns int
			var projectHooks []string
			for _, item := range items {
				if item.Data == nil {
					continue
				}
				data, ok := item.Data.(*CompletionItemData)
				require.True(t, ok)
				if data.Definition == nil || data.Definition.Package == nil {
					continue
				}
				if item.Label == "turn" && *data.Definition.Package == SpxPkgPath {
					nativeTurns++
				}
				if *data.Definition.Package == "main" && (strings.Contains(strings.ToLower(item.Label), "turn") || strings.Contains(item.Label, "reportLita")) {
					projectHooks = append(projectHooks, item.Label)
				}
			}
			assert.Equal(t, 3, nativeTurns)
			if tt.wantHooks {
				assert.Len(t, projectHooks, 5)
			} else {
				assert.Empty(t, projectHooks)
			}
		})
	}
}
