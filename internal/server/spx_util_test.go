package server

import (
	gotypes "go/types"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/stretchr/testify/assert"
)

func TestPropertyTargetNamedTypeForCall(t *testing.T) {
	newNamed := func(pkg *gotypes.Package, name string) *gotypes.Named {
		return gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, pkg, name, nil), gotypes.NewStruct(nil, nil), nil)
	}

	for _, tt := range []struct {
		name       string
		build      func() (*types.Info, *ast.CallExpr, string, string)
		wantName   string
		wantResult bool
	}{
		{
			name: "SelectorReceiverNamed",
			build: func() (*types.Info, *ast.CallExpr, string, string) {
				pkg := gotypes.NewPackage("example.com/test", "test")
				sprite := newNamed(pkg, "Sprite")

				recvIdent := &ast.Ident{Name: "sprite"}
				info := &types.Info{Pkg: pkg}
				info.Types = map[ast.Expr]gotypes.TypeAndValue{
					recvIdent: {Type: sprite},
				}

				call := &ast.CallExpr{Fun: &ast.SelectorExpr{X: recvIdent, Sel: &ast.Ident{Name: "Show"}}}
				return info, call, "Sprite.spx", "main.spx"
			},
			wantName:   "Sprite",
			wantResult: true,
		},
		{
			name: "SelectorReceiverPointer",
			build: func() (*types.Info, *ast.CallExpr, string, string) {
				pkg := gotypes.NewPackage("example.com/test", "test")
				sprite := newNamed(pkg, "Sprite")

				recvIdent := &ast.Ident{Name: "sprite"}
				info := &types.Info{Pkg: pkg}
				info.Types = map[ast.Expr]gotypes.TypeAndValue{
					recvIdent: {Type: gotypes.NewPointer(sprite)},
				}

				call := &ast.CallExpr{Fun: &ast.SelectorExpr{X: recvIdent, Sel: &ast.Ident{Name: "Show"}}}
				return info, call, "Sprite.spx", "main.spx"
			},
			wantName:   "Sprite",
			wantResult: true,
		},
		{
			name: "SelectorReceiverCallExpr",
			build: func() (*types.Info, *ast.CallExpr, string, string) {
				pkg := gotypes.NewPackage("example.com/test", "test")
				sprite := newNamed(pkg, "Sprite")

				getSprite := &ast.CallExpr{Fun: &ast.Ident{Name: "getSprite"}}
				info := &types.Info{Pkg: pkg}
				info.Types = map[ast.Expr]gotypes.TypeAndValue{
					getSprite: {Type: sprite},
				}

				call := &ast.CallExpr{Fun: &ast.SelectorExpr{X: getSprite, Sel: &ast.Ident{Name: "Show"}}}
				return info, call, "Sprite.spx", "main.spx"
			},
			wantName:   "Sprite",
			wantResult: true,
		},
		{
			name: "SelectorReceiverMissingType",
			build: func() (*types.Info, *ast.CallExpr, string, string) {
				pkg := gotypes.NewPackage("example.com/test", "test")

				recvIdent := &ast.Ident{Name: "sprite"}
				info := &types.Info{Pkg: pkg}

				call := &ast.CallExpr{Fun: &ast.SelectorExpr{X: recvIdent, Sel: &ast.Ident{Name: "Show"}}}
				return info, call, "Sprite.spx", "main.spx"
			},
			wantResult: false,
		},
		{
			name: "SelectorReceiverNonNamedType",
			build: func() (*types.Info, *ast.CallExpr, string, string) {
				pkg := gotypes.NewPackage("example.com/test", "test")

				recvIdent := &ast.Ident{Name: "s"}
				info := &types.Info{Pkg: pkg}
				info.Types = map[ast.Expr]gotypes.TypeAndValue{
					recvIdent: {Type: gotypes.Typ[gotypes.String]},
				}

				call := &ast.CallExpr{Fun: &ast.SelectorExpr{X: recvIdent, Sel: &ast.Ident{Name: "Show"}}}
				return info, call, "Sprite.spx", "main.spx"
			},
			wantResult: false,
		},
		{
			name: "ImplicitMainSpxResolvesGame",
			build: func() (*types.Info, *ast.CallExpr, string, string) {
				pkg := gotypes.NewPackage("example.com/test", "test")
				game := newNamed(pkg, "Game")
				_ = pkg.Scope().Insert(game.Obj())

				info := &types.Info{Pkg: pkg}
				call := &ast.CallExpr{Fun: &ast.Ident{Name: "showVar"}}
				return info, call, "dir/main.spx", "main.spx"
			},
			wantName:   "Game",
			wantResult: true,
		},
		{
			name: "ImplicitSpriteFileResolvesTypeByFileName",
			build: func() (*types.Info, *ast.CallExpr, string, string) {
				pkg := gotypes.NewPackage("example.com/test", "test")
				hero := newNamed(pkg, "Hero")
				_ = pkg.Scope().Insert(hero.Obj())

				info := &types.Info{Pkg: pkg}
				call := &ast.CallExpr{Fun: &ast.Ident{Name: "showVar"}}
				return info, call, "dir/Hero.spx", "main.spx"
			},
			wantName:   "Hero",
			wantResult: true,
		},
		{
			name: "ImplicitLookupNotTypeName",
			build: func() (*types.Info, *ast.CallExpr, string, string) {
				pkg := gotypes.NewPackage("example.com/test", "test")
				_ = pkg.Scope().Insert(gotypes.NewVar(token.NoPos, pkg, "Enemy", gotypes.Typ[gotypes.Int]))

				info := &types.Info{Pkg: pkg}
				call := &ast.CallExpr{Fun: &ast.Ident{Name: "showVar"}}
				return info, call, "dir/Enemy.spx", "main.spx"
			},
			wantResult: false,
		},
		{
			name: "ImplicitEmptyTypeName",
			build: func() (*types.Info, *ast.CallExpr, string, string) {
				pkg := gotypes.NewPackage("example.com/test", "test")
				info := &types.Info{Pkg: pkg}
				call := &ast.CallExpr{Fun: &ast.Ident{Name: "showVar"}}
				return info, call, "dir/.spx", "main.spx"
			},
			wantResult: false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			info, call, spxFile, mainSpxFile := tt.build()
			got := PropertyTargetNamedTypeForCall(info, call, spxFile, mainSpxFile)
			if !tt.wantResult {
				assert.Nil(t, got)
				return
			}
			if assert.NotNil(t, got) {
				assert.Equal(t, tt.wantName, got.Obj().Name())
			}
		})
	}
}

func TestIsInSpxPkg(t *testing.T) {
	t.Run("NilObject", func(t *testing.T) {
		assert.False(t, IsInSpxPkg(nil))
	})

	for _, tt := range []struct {
		name string
		pkg  *gotypes.Package
	}{
		{name: "NoPackage"},
		{name: "OtherPackageNamedSpx", pkg: gotypes.NewPackage("example.com/spx", "spx")},
		{name: "SpxSubpackage", pkg: gotypes.NewPackage(SpxPkgPath+"/subpkg", "subpkg")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			obj := gotypes.NewTypeName(token.NoPos, tt.pkg, "Value", gotypes.Typ[gotypes.Int])
			assert.False(t, IsInSpxPkg(obj))
		})
	}
}

func TestGetSimplifiedTypeString(t *testing.T) {
	for _, tt := range []struct {
		name string
		pkg  *gotypes.Package
		want string
	}{
		{name: "NoPackage", want: "Value"},
		{name: "OtherPackage", pkg: gotypes.NewPackage("example.com/sample", "sample"), want: "sample.Value"},
		{name: "OtherPackageNamedSpx", pkg: gotypes.NewPackage("example.com/spx", "spx"), want: "spx.Value"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			named := gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, tt.pkg, "Value", nil), gotypes.NewStruct(nil, nil), nil)
			assert.Equal(t, tt.want, GetSimplifiedTypeString(named))
		})
	}
}
