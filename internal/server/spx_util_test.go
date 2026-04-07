package server

import (
	"go/token"
	"go/types"
	"testing"

	xgoast "github.com/goplus/xgo/ast"
	xgotypes "github.com/goplus/xgolsw/xgo/types"
	"github.com/stretchr/testify/assert"
)

func TestResolvedNamedType(t *testing.T) {
	pkg := types.NewPackage("example.com/pkg", "pkg")
	named := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "Point", nil), types.NewStruct(nil, nil), nil)
	aliasToNamed := types.NewAlias(types.NewTypeName(token.NoPos, pkg, "PointAlias", nil), named)
	aliasChainToNamed := types.NewAlias(types.NewTypeName(token.NoPos, pkg, "PointAliasChain", nil), aliasToNamed)
	aliasToBasic := types.NewAlias(types.NewTypeName(token.NoPos, pkg, "StringAlias", nil), types.Typ[types.String])
	aliasToPointerNamed := types.NewAlias(types.NewTypeName(token.NoPos, pkg, "PointPtrAlias", nil), types.NewPointer(named))
	aliasChainToPointerNamed := types.NewAlias(types.NewTypeName(token.NoPos, pkg, "PointPtrAliasChain", nil), aliasToPointerNamed)

	for _, tt := range []struct {
		name string
		typ  types.Type
		want *types.Named
	}{
		{
			name: "Nil",
			typ:  nil,
			want: nil,
		},
		{
			name: "Named",
			typ:  named,
			want: named,
		},
		{
			name: "PointerToNamed",
			typ:  types.NewPointer(named),
			want: named,
		},
		{
			name: "AliasToNamed",
			typ:  aliasToNamed,
			want: named,
		},
		{
			name: "PointerToAliasToNamed",
			typ:  types.NewPointer(aliasToNamed),
			want: named,
		},
		{
			name: "AliasChainToNamed",
			typ:  aliasChainToNamed,
			want: named,
		},
		{
			name: "Basic",
			typ:  types.Typ[types.Int],
			want: nil,
		},
		{
			name: "AliasToBasic",
			typ:  aliasToBasic,
			want: nil,
		},
		{
			name: "AliasToPointerNamed",
			typ:  aliasToPointerNamed,
			want: named,
		},
		{
			name: "AliasChainToPointerNamed",
			typ:  aliasChainToPointerNamed,
			want: named,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := resolvedNamedType(tt.typ)
			if tt.want == nil {
				assert.Nil(t, got)
				return
			}
			assert.Same(t, tt.want, got)
		})
	}
}

func TestSpxFileNameHelpers(t *testing.T) {
	for _, tt := range []struct {
		name         string
		spxFile      string
		mainSpxFile  string
		wantTypeName string
		wantSprite   string
	}{
		{
			name:         "MainFile",
			spxFile:      "dir/main.spx",
			mainSpxFile:  "main.spx",
			wantTypeName: "Game",
			wantSprite:   "",
		},
		{
			name:         "SpriteFile",
			spxFile:      "dir/Hero.spx",
			mainSpxFile:  "main.spx",
			wantTypeName: "Hero",
			wantSprite:   "Hero",
		},
		{
			name:         "EmptyFile",
			spxFile:      "",
			mainSpxFile:  "main.spx",
			wantTypeName: "",
			wantSprite:   "",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantTypeName, spxWorkTypeNameForFile(tt.spxFile, tt.mainSpxFile))
			assert.Equal(t, tt.wantSprite, spxSpriteNameForFile(tt.spxFile, tt.mainSpxFile))
		})
	}
}

func TestPropertyTargetNamedTypeForCall(t *testing.T) {
	newNamed := func(pkg *types.Package, name string) *types.Named {
		return types.NewNamed(types.NewTypeName(token.NoPos, pkg, name, nil), types.NewStruct(nil, nil), nil)
	}

	for _, tt := range []struct {
		name       string
		build      func() (*xgotypes.Info, *xgoast.CallExpr, string, string)
		wantName   string
		wantResult bool
	}{
		{
			name: "SelectorReceiverNamed",
			build: func() (*xgotypes.Info, *xgoast.CallExpr, string, string) {
				pkg := types.NewPackage("example.com/test", "test")
				sprite := newNamed(pkg, "Sprite")

				recvIdent := &xgoast.Ident{Name: "sprite"}
				info := &xgotypes.Info{Pkg: pkg}
				info.Types = map[xgoast.Expr]types.TypeAndValue{
					recvIdent: {Type: sprite},
				}

				call := &xgoast.CallExpr{Fun: &xgoast.SelectorExpr{X: recvIdent, Sel: &xgoast.Ident{Name: "Show"}}}
				return info, call, "Sprite.spx", "main.spx"
			},
			wantName:   "Sprite",
			wantResult: true,
		},
		{
			name: "SelectorReceiverPointer",
			build: func() (*xgotypes.Info, *xgoast.CallExpr, string, string) {
				pkg := types.NewPackage("example.com/test", "test")
				sprite := newNamed(pkg, "Sprite")

				recvIdent := &xgoast.Ident{Name: "sprite"}
				info := &xgotypes.Info{Pkg: pkg}
				info.Types = map[xgoast.Expr]types.TypeAndValue{
					recvIdent: {Type: types.NewPointer(sprite)},
				}

				call := &xgoast.CallExpr{Fun: &xgoast.SelectorExpr{X: recvIdent, Sel: &xgoast.Ident{Name: "Show"}}}
				return info, call, "Sprite.spx", "main.spx"
			},
			wantName:   "Sprite",
			wantResult: true,
		},
		{
			name: "SelectorReceiverCallExpr",
			build: func() (*xgotypes.Info, *xgoast.CallExpr, string, string) {
				pkg := types.NewPackage("example.com/test", "test")
				sprite := newNamed(pkg, "Sprite")

				getSprite := &xgoast.CallExpr{Fun: &xgoast.Ident{Name: "getSprite"}}
				info := &xgotypes.Info{Pkg: pkg}
				info.Types = map[xgoast.Expr]types.TypeAndValue{
					getSprite: {Type: sprite},
				}

				call := &xgoast.CallExpr{Fun: &xgoast.SelectorExpr{X: getSprite, Sel: &xgoast.Ident{Name: "Show"}}}
				return info, call, "Sprite.spx", "main.spx"
			},
			wantName:   "Sprite",
			wantResult: true,
		},
		{
			name: "SelectorReceiverMissingType",
			build: func() (*xgotypes.Info, *xgoast.CallExpr, string, string) {
				pkg := types.NewPackage("example.com/test", "test")

				recvIdent := &xgoast.Ident{Name: "sprite"}
				info := &xgotypes.Info{Pkg: pkg}

				call := &xgoast.CallExpr{Fun: &xgoast.SelectorExpr{X: recvIdent, Sel: &xgoast.Ident{Name: "Show"}}}
				return info, call, "Sprite.spx", "main.spx"
			},
			wantResult: false,
		},
		{
			name: "SelectorReceiverNonNamedType",
			build: func() (*xgotypes.Info, *xgoast.CallExpr, string, string) {
				pkg := types.NewPackage("example.com/test", "test")

				recvIdent := &xgoast.Ident{Name: "s"}
				info := &xgotypes.Info{Pkg: pkg}
				info.Types = map[xgoast.Expr]types.TypeAndValue{
					recvIdent: {Type: types.Typ[types.String]},
				}

				call := &xgoast.CallExpr{Fun: &xgoast.SelectorExpr{X: recvIdent, Sel: &xgoast.Ident{Name: "Show"}}}
				return info, call, "Sprite.spx", "main.spx"
			},
			wantResult: false,
		},
		{
			name: "ImplicitMainSpxResolvesGame",
			build: func() (*xgotypes.Info, *xgoast.CallExpr, string, string) {
				pkg := types.NewPackage("example.com/test", "test")
				game := newNamed(pkg, "Game")
				_ = pkg.Scope().Insert(game.Obj())

				info := &xgotypes.Info{Pkg: pkg}
				call := &xgoast.CallExpr{Fun: &xgoast.Ident{Name: "showVar"}}
				return info, call, "dir/main.spx", "main.spx"
			},
			wantName:   "Game",
			wantResult: true,
		},
		{
			name: "ImplicitSpriteFileResolvesTypeByFileName",
			build: func() (*xgotypes.Info, *xgoast.CallExpr, string, string) {
				pkg := types.NewPackage("example.com/test", "test")
				hero := newNamed(pkg, "Hero")
				_ = pkg.Scope().Insert(hero.Obj())

				info := &xgotypes.Info{Pkg: pkg}
				call := &xgoast.CallExpr{Fun: &xgoast.Ident{Name: "showVar"}}
				return info, call, "dir/Hero.spx", "main.spx"
			},
			wantName:   "Hero",
			wantResult: true,
		},
		{
			name: "ImplicitLookupNotTypeName",
			build: func() (*xgotypes.Info, *xgoast.CallExpr, string, string) {
				pkg := types.NewPackage("example.com/test", "test")
				_ = pkg.Scope().Insert(types.NewVar(token.NoPos, pkg, "Enemy", types.Typ[types.Int]))

				info := &xgotypes.Info{Pkg: pkg}
				call := &xgoast.CallExpr{Fun: &xgoast.Ident{Name: "showVar"}}
				return info, call, "dir/Enemy.spx", "main.spx"
			},
			wantResult: false,
		},
		{
			name: "ImplicitEmptyTypeName",
			build: func() (*xgotypes.Info, *xgoast.CallExpr, string, string) {
				pkg := types.NewPackage("example.com/test", "test")
				info := &xgotypes.Info{Pkg: pkg}
				call := &xgoast.CallExpr{Fun: &xgoast.Ident{Name: "showVar"}}
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
