package server

import (
	"go/constant"
	gotypes "go/types"
	"testing"

	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo/xgoutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// xgoxTestImporter provides a partial-type-argument XGox function.
type xgoxTestImporter struct {
	fallback gotypes.Importer
}

// Import implements [gotypes.Importer].
func (i xgoxTestImporter) Import(path string) (*gotypes.Package, error) {
	if path != "example.com/typeargs" {
		return i.fallback.Import(path)
	}

	pkg := gotypes.NewPackage(path, "typeargs")
	pkg.Scope().Insert(gotypes.NewConst(
		token.NoPos,
		pkg,
		xgoutil.XGoPackage,
		gotypes.Typ[gotypes.UntypedBool],
		constant.MakeBool(true),
	))

	constraint := gotypes.NewInterfaceType(nil, nil)
	constraint.Complete()
	fromConstraint := gotypes.NewInterfaceType(nil, []gotypes.Type{
		gotypes.NewUnion([]*gotypes.Term{gotypes.NewTerm(true, gotypes.Typ[gotypes.Int])}),
	})
	fromConstraint.Complete()
	toTypeParam := gotypes.NewTypeParam(gotypes.NewTypeName(token.NoPos, pkg, "To", nil), constraint)
	fromTypeParam := gotypes.NewTypeParam(gotypes.NewTypeName(token.NoPos, pkg, "From", nil), fromConstraint)
	pkg.Scope().Insert(gotypes.NewFunc(
		token.NoPos,
		pkg,
		"XGox_Convert",
		gotypes.NewSignatureType(
			nil,
			nil,
			[]*gotypes.TypeParam{toTypeParam, fromTypeParam},
			gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "src", fromTypeParam)),
			gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "", toTypeParam)),
			false,
		),
	))
	pkg.MarkComplete()
	return pkg, nil
}

func TestDisplayedFuncNameXGox(t *testing.T) {
	pkg, err := (xgoxTestImporter{}).Import("example.com/typeargs")
	require.NoError(t, err)
	fun, ok := pkg.Scope().Lookup("XGox_Convert").(*gotypes.Func)
	require.True(t, ok)

	overview, recvTypeName, name, overloadID := makeSpxDefinitionOverviewForFunc(fun)
	assert.Equal(t, "func convert(To Type, From Type, src From) To", overview)
	assert.Empty(t, recvTypeName)
	assert.Equal(t, "convert", name)
	assert.Nil(t, overloadID)
}
