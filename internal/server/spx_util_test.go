package server

import (
	gotypes "go/types"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/stretchr/testify/assert"
)

func TestResolvedNamedType(t *testing.T) {
	pkg := gotypes.NewPackage("example.com/pkg", "pkg")
	named := gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, pkg, "Point", nil), gotypes.NewStruct(nil, nil), nil)
	aliasToNamed := gotypes.NewAlias(gotypes.NewTypeName(token.NoPos, pkg, "PointAlias", nil), named)
	aliasChainToNamed := gotypes.NewAlias(gotypes.NewTypeName(token.NoPos, pkg, "PointAliasChain", nil), aliasToNamed)
	aliasToBasic := gotypes.NewAlias(gotypes.NewTypeName(token.NoPos, pkg, "StringAlias", nil), gotypes.Typ[gotypes.String])
	aliasToPointerNamed := gotypes.NewAlias(gotypes.NewTypeName(token.NoPos, pkg, "PointPtrAlias", nil), gotypes.NewPointer(named))
	aliasChainToPointerNamed := gotypes.NewAlias(gotypes.NewTypeName(token.NoPos, pkg, "PointPtrAliasChain", nil), aliasToPointerNamed)

	for _, tt := range []struct {
		name string
		typ  gotypes.Type
		want *gotypes.Named
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
			typ:  gotypes.NewPointer(named),
			want: named,
		},
		{
			name: "AliasToNamed",
			typ:  aliasToNamed,
			want: named,
		},
		{
			name: "PointerToAliasToNamed",
			typ:  gotypes.NewPointer(aliasToNamed),
			want: named,
		},
		{
			name: "AliasChainToNamed",
			typ:  aliasChainToNamed,
			want: named,
		},
		{
			name: "Basic",
			typ:  gotypes.Typ[gotypes.Int],
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
