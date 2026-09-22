package server

import (
	goast "go/ast"
	goparser "go/parser"
	gotypes "go/types"
	"testing"

	"github.com/goplus/mod/modfile"
	"github.com/goplus/mod/modload"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/internal"
	"github.com/goplus/xgolsw/internal/pkgdata"
	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/require"
)

// newSpxTestServer supplies only the SDK declarations used by adapter tests.
// Imports and documentation are created from source, without embedded package data.
func newSpxTestServer(t testing.TB, files map[string][]byte) *Server {
	t.Helper()

	s := newTestServer(t, files)
	proj := s.getProj()
	file, err := goparser.ParseFile(proj.Fset, "spx.go", spxTestSource, goparser.ParseComments)
	require.NoError(t, err)
	pkg, err := new(gotypes.Config).Check(SpxPkgPath, proj.Fset, []*goast.File{file}, nil)
	require.NoError(t, err)
	doc := pkgdoc.NewGo(SpxPkgPath, &goast.Package{Name: pkg.Name(), Files: map[string]*goast.File{"spx.go": file}})
	data, err := pkgdata.New(testframework.NewPkgDataZip(t))
	require.NoError(t, err)
	fmtPkg, err := internal.NewImporter(data.OpenExport).Import("fmt")
	require.NoError(t, err)
	baseImporter, baseLookup := proj.Importer, s.lookupPkgDoc
	proj.Importer = testImporterFunc(func(path string) (*gotypes.Package, error) {
		if path == SpxPkgPath {
			return pkg, nil
		}
		if path == "fmt" {
			return fmtPkg, nil
		}
		return baseImporter.Import(path)
	})
	s.listPkgs = func() ([]string, error) { return []string{SpxPkgPath}, nil }
	s.lookupPkgDoc = func(path string) (*pkgdoc.PkgDoc, error) {
		if path == SpxPkgPath {
			return doc, nil
		}
		return baseLookup(path)
	}
	proj.SetModule(newTestModule(t, modload.Module{Opt: &modfile.File{Projects: []*modfile.Project{{
		Ext: ".spx", FullExt: "main.spx", Class: "Game", PkgPaths: []string{SpxPkgPath},
		Works: []*modfile.Class{{Ext: ".spx", Class: "SpriteImpl", Embedded: true}},
	}}}}))
	return s
}

func spxTestType(t testing.TB, s *Server, name string) gotypes.Type {
	t.Helper()

	pkg, err := s.getProj().Importer.Import(SpxPkgPath)
	require.NoError(t, err)
	obj := pkg.Scope().Lookup(name)
	require.NotNil(t, obj)
	return obj.Type()
}

const spxTestSource = `package spx
const XGoPackage = true

type BackdropName = string
type SoundName = string
type SpriteName = string
type SpriteCostumeName = string
type SpriteAnimationName = string
type WidgetName = string
type PropertyName = string
type Direction = float64
type Key = int
type layerAction int
type dirAction int
type EffectKind int
type specialObj int
type RotationStyle int
const (
    Left Direction = -90
    Right Direction = 90
    Front layerAction = 0
    Forward dirAction = 0
    ColorEffect EffectKind = 0
    Edge specialObj = 0
    Mouse specialObj = 1
    LeftRight RotationStyle = 0
    KeySpace Key = 32
    Key1 Key = 49
)

type Color struct{}
func HSB(h, s, b float64) Color { return Color{} }
func HSBA(h, s, b, a float64) Color { return Color{} }
type Value struct{}
type List struct{}
func NewList(values ...any) List { return List{} }

type eventBindings struct{}
func (*eventBindings) OnStart(onStart func()) {}
func (*eventBindings) OnKey__0(key Key, onKey func()) {}
func (*eventBindings) OnKey__1(keys []Key, onKey func(Key)) {}
func (*eventBindings) OnKey__2(keys []Key, onKey func()) {}
func (*eventBindings) OnBackdrop__0(onBackdrop func(BackdropName)) {}
func (*eventBindings) OnBackdrop__1(name BackdropName, onBackdrop func()) {}

type Game struct { eventBindings }
func (*Game) initGame() {}
func XGot_Game_Main(game interface{ initGame() }, sprites ...Sprite) {}
func (*Game) Play__0(name SoundName) {}
func (*Game) Play__1(name SoundName, loop bool) {}
func (*Game) SetBackdrop__0(name BackdropName) {}
func (*Game) SetBackdrop__1(index int) {}
func (*Game) BackdropName() BackdropName { return "" }
func (*Game) ShowVar(name PropertyName) {}
func (*Game) HideVar(name PropertyName) {}
// Volume returns the game volume.
func (*Game) Volume() float64 { return 0 }

type Sprite interface {
    Main()
    initSprite()
    SetCostume__0(SpriteCostumeName)
    Animate__0(SpriteAnimationName)
    StepTo__0(Sprite)
    StepTo__1(SpriteName)
}
type SpriteImpl struct { eventBindings }
func (*SpriteImpl) OnClick(onClick func()) {}
func (*SpriteImpl) Main() {}
func (*SpriteImpl) initSprite() {}
func (*SpriteImpl) Play__0(name SoundName) {}
func (*SpriteImpl) Play__1(name SoundName, loop bool) {}
func (*SpriteImpl) SetCostume__0(costume SpriteCostumeName) {}
func (*SpriteImpl) SetCostume__1(index float64) {}
func (*SpriteImpl) Animate__0(name SpriteAnimationName) {}
func (*SpriteImpl) StepTo__0(sprite Sprite) {}
func (*SpriteImpl) StepTo__1(sprite SpriteName) {}
func (*SpriteImpl) TurnTo__0(target Sprite) {}
func (*SpriteImpl) TurnTo__1(target SpriteName) {}
func (*SpriteImpl) Touching__0(sprite Sprite) bool { return false }
func (*SpriteImpl) Touching__1(sprite SpriteName) bool { return false }
func (*SpriteImpl) Turn__0(dir Direction) {}
func (*SpriteImpl) Heading() Direction { return 0 }
func (*SpriteImpl) Clone__0() {}
func (*SpriteImpl) Clone__1(data any) {}
func (*SpriteImpl) ShowVar(name PropertyName) {}
func (*SpriteImpl) HideVar(name PropertyName) {}
func (*SpriteImpl) Xpos() float64 { return 0 }
// Volume returns the sprite volume.
func (*SpriteImpl) Volume() float64 { return 0 }

type Monitor struct{}
func XGot_Game_XGox_GetWidget[T any](game any, name WidgetName) *T { return nil }
`

// analyzeSpxTestFile preserves partial ASTs for editor tests with syntax errors.
func analyzeSpxTestFile(t testing.TB, s *Server, filename string) (*spxAnalysis, *ast.File) {
	t.Helper()

	result, err := analyzeSpx(s.getProj())
	require.NoError(t, err)
	file, _ := s.getProj().ASTFile(filename)
	require.NotNil(t, file)
	return result, file
}

func analyzeSpx(proj *xgo.Project) (*spxAnalysis, error) {
	result, err := loadSpxAnalysis(proj)
	if err != nil || result == nil {
		return result, err
	}
	collectResourceReferences(proj, []*resourceProvider{result.resourceProvider()})
	return result, nil
}
