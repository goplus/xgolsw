package server

import (
	gotypes "go/types"
	"testing"

	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
)

func TestDefinitionContextSpxResourceNameType(t *testing.T) {
	s := newSpxTestServer(t, nil)
	ctx := &definitionContext{proj: s.getProj()}
	pkg := gotypes.NewPackage("example.com/pkg", "pkg")
	soundAlias := gotypes.NewAlias(
		gotypes.NewTypeName(token.NoPos, pkg, "MySoundName", nil),
		spxTestType(t, s, "SoundName"),
	)
	soundAliasChain := gotypes.NewAlias(
		gotypes.NewTypeName(token.NoPos, pkg, "MySoundNameChain", nil),
		soundAlias,
	)

	for _, tt := range []struct {
		name string
		typ  gotypes.Type
		want string
	}{
		{
			name: "Nil",
			typ:  nil,
			want: "",
		},
		{
			name: "DirectBackdropName",
			typ:  spxTestType(t, s, "BackdropName"),
			want: "BackdropName",
		},
		{
			name: "AliasToSoundName",
			typ:  soundAlias,
			want: "SoundName",
		},
		{
			name: "AliasChainToSoundName",
			typ:  soundAliasChain,
			want: "SoundName",
		},
		{
			name: "BasicString",
			typ:  gotypes.Typ[gotypes.String],
			want: "",
		},
		{
			name: "AliasToBasicString",
			typ:  gotypes.NewAlias(gotypes.NewTypeName(token.NoPos, pkg, "MyString", nil), gotypes.Typ[gotypes.String]),
			want: "",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ctx.spxResourceNameType(tt.typ))
		})
	}
}

func TestDefinitionContextSpxTypesFromProject(t *testing.T) {
	s := newSpxTestServer(t, nil)
	other := newSpxTestServer(t, nil)
	ctx := &definitionContext{proj: s.getProj()}
	for _, name := range []string{"SoundName", "PropertyName", "Value", "List", "Sprite", "SpriteImpl"} {
		t.Run(name, func(t *testing.T) {
			own := spxTestType(t, s, name)
			foreign := spxTestType(t, other, name)
			assert.Equal(t, name, ctx.spxTypeName(own))
			assert.Empty(t, ctx.spxTypeName(foreign))
			local := gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, nil, name, nil), gotypes.Unalias(own).Underlying(), nil)
			assert.Empty(t, ctx.spxTypeName(local))
			alias := gotypes.NewAlias(gotypes.NewTypeName(token.NoPos, nil, "Alias", nil), own)
			assert.Equal(t, name, ctx.spxTypeName(alias))
		})
	}

	t.Run("ImporterChange", func(t *testing.T) {
		proj := s.getProj().Snapshot()
		proj.Importer = other.getProj().Importer
		next := &definitionContext{proj: proj}
		assert.Empty(t, next.spxTypeName(spxTestType(t, s, "SoundName")))
		assert.Equal(t, "SoundName", next.spxTypeName(spxTestType(t, other, "SoundName")))
		assert.Equal(t, "SoundName", ctx.spxTypeName(spxTestType(t, s, "SoundName")))
	})

	t.Run("UnavailableSDK", func(t *testing.T) {
		proj := s.getProj().Snapshot()
		proj.Importer = completionTestImporter{Importer: proj.Importer, unavailablePath: SpxPkgPath}
		ctx := &definitionContext{proj: proj}
		assert.Empty(t, ctx.spxResourceNameType(spxTestType(t, s, "SoundName")))
		assert.False(t, ctx.isSpxPropertyNameType(spxTestType(t, s, "PropertyName")))
		assert.False(t, ctx.isSpxValueOrListType(requireValueAs[*gotypes.Named](t, spxTestType(t, s, "Value"))))
	})

	t.Run("PartialSDK", func(t *testing.T) {
		s := newSpxSymbolTestServer(t, nil, SpxPkgPath)
		setSpxSymbolTestModule(t, s, SpxPkgPath)
		ctx := &definitionContext{proj: s.getProj()}
		assert.Equal(t, "SpriteImpl", ctx.spxTypeName(spxTestType(t, s, "SpriteImpl")))
		assert.Empty(t, ctx.spxResourceNameType(gotypes.Typ[gotypes.String]))
	})
}
