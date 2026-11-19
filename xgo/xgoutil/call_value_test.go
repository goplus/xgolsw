package xgoutil

import (
	goast "go/ast"
	"go/constant"
	goparser "go/parser"
	gotoken "go/token"
	gotypes "go/types"
	"testing"

	"github.com/goplus/gogen"
	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCallValueTypeMatches(t *testing.T) {
	integer := gotypes.Typ[gotypes.Int]
	union := gotypes.NewUnion([]*gotypes.Term{gotypes.NewTerm(true, integer)})
	constraint := gotypes.NewInterfaceType(nil, []gotypes.Type{union}).Complete()
	typeParam := gotypes.NewTypeParam(gotypes.NewTypeName(token.NoPos, nil, "T", nil), constraint)
	named := gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, nil, "Item", nil), gotypes.NewStruct(nil, nil), nil)
	for _, tt := range []struct {
		name         string
		actual, want gotypes.Type
		matches      bool
	}{
		{"UnknownOperand", nil, integer, false},
		{"SatisfiesConstraint", integer, typeParam, true},
		{"OutsideConstraint", gotypes.Typ[gotypes.String], typeParam, false},
		{"NilToPointer", gotypes.Typ[gotypes.UntypedNil], gotypes.NewPointer(named), true},
		{"PointerToUnnamedTarget", gotypes.NewPointer(named), gotypes.NewPointer(integer), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			value := gotypes.TypeAndValue{Type: tt.actual}
			assert.Equal(t, tt.matches, CallValueTypeMatches(nil, value, tt.want))
		})
	}
}

func TestCallValueTypeMatchesNumericConstant(t *testing.T) {
	for _, tt := range []struct {
		name     string
		value    constant.Value
		from, to gotypes.BasicKind
		want     bool
	}{
		{"IntegralFloat", constant.MakeFloat64(42), gotypes.UntypedFloat, gotypes.Uint8, true},
		{"FractionalFloat", constant.MakeFloat64(1.5), gotypes.UntypedFloat, gotypes.Uint8, false},
		{"OutOfRangeFloat", constant.MakeFloat64(256), gotypes.UntypedFloat, gotypes.Uint8, false},
		{"ComplexToComplex", constant.MakeImag(constant.MakeInt64(1)), gotypes.UntypedComplex, gotypes.Complex64, true},
		{"ComplexToFloat", constant.MakeImag(constant.MakeInt64(1)), gotypes.UntypedComplex, gotypes.Float64, false},
		{"ComplexToInt", constant.MakeImag(constant.MakeInt64(1)), gotypes.UntypedComplex, gotypes.Int, false},
		{"Complex128", constant.MakeImag(constant.MakeFloat64(1e300)), gotypes.UntypedComplex, gotypes.Complex128, true},
		{"Float64Overflow", constant.MakeFromLiteral("1e400", gotoken.FLOAT, 0), gotypes.UntypedFloat, gotypes.Float64, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			value := gotypes.TypeAndValue{Type: gotypes.Typ[tt.from], Value: tt.value}
			assert.Equal(t, tt.want, CallValueTypeMatches(nil, value, gotypes.Typ[tt.to]))
		})
	}
}

func TestCallValueTypeMatchesInitializer(t *testing.T) {
	const declarations = `package fixture
const XGoPackage = true

type Amount struct{}
type Alias = Amount
func Amount_Init(value uint8) Amount { return Amount{} }

type Limit struct{}
const Limit_Min = 1
const Limit_Max = 10
func Limit_Init(value int) Limit { return Limit{} }

type Chain struct{}
func Chain_Init(value Amount) Chain { return Chain{} }

type Cycle struct{}
type Peer struct{}
func Cycle_Init(value Peer) Cycle { return Cycle{} }
func Peer_Init(value Cycle) Peer { return Peer{} }

type Extra struct{}
func Extra_Init(value uint8, label string) Extra { return Extra{} }
type Empty struct{}
func Empty_Init() Empty { return Empty{} }

type Optional struct{}
func Optional_Init(value uint8, __xgo_optional_label string) Optional { return Optional{} }
type Spread struct{}
func Spread_Init(values ...uint8) Spread { return Spread{} }

type Generic struct{}
func Generic_Init[T ~int | ~string](value T) Generic { return Generic{} }

type Explicit struct{}
func XGox_Explicit_Init[T any](value T) Explicit { return Explicit{} }

type FunctionValue struct{}
type Initializer func(value uint8) FunctionValue
var FunctionValue_Init Initializer

type Choice struct{}
func Choice_Init__0(value int) Choice { return Choice{} }
func Choice_Init__1(value string) Choice { return Choice{} }

type Base struct{}
type Middle struct{ *Base }
type Embedded struct {
	Field int
	Middle
}
type Recursive struct {
	*Recursive
	Field Base
}
type Wrapped struct{}
func Wrapped_Init(value *Base) Wrapped { return Wrapped{} }
`
	fset := token.NewFileSet()
	file, err := goparser.ParseFile(fset, "initializers.go", declarations, 0)
	require.NoError(t, err)
	pkg, err := new(gotypes.Config).Check("example.com/fixture", fset, []*goast.File{file}, nil)
	require.NoError(t, err)
	gogen.InitXGoPackage(pkg)
	typ := func(name string) gotypes.Type {
		obj := pkg.Scope().Lookup(name)
		require.NotNil(t, obj)
		return obj.Type()
	}
	uint8Type := gotypes.Typ[gotypes.Uint8]
	boolType := gotypes.Typ[gotypes.Bool]
	intType := gotypes.Typ[gotypes.Int]
	stringType := gotypes.Typ[gotypes.String]
	untypedInt := gotypes.Typ[gotypes.UntypedInt]
	untypedFloat := gotypes.Typ[gotypes.UntypedFloat]
	for _, tt := range []struct {
		name, target string
		actual       gotypes.Type
		value        constant.Value
		want         bool
	}{
		{"ExactInput", "Amount", uint8Type, nil, true},
		{"IncompatibleInput", "Amount", boolType, nil, false},
		{"AliasedTarget", "Alias", uint8Type, nil, true},
		{"RepresentableConstant", "Amount", untypedInt, constant.MakeInt64(255), true},
		{"OverflowingInput", "Amount", untypedInt, constant.MakeInt64(256), false},
		{"UpperBound", "Limit", untypedInt, constant.MakeInt64(11), false},
		{"LowerBound", "Limit", untypedInt, constant.MakeInt64(0), false},
		{"WithinBounds", "Limit", untypedFloat, constant.MakeFloat64(10), true},
		{"FractionalInput", "Limit", untypedFloat, constant.MakeFloat64(9.5), false},
		{"NestedInitializer", "Chain", uint8Type, nil, true},
		{"NestedIncompatibleInput", "Chain", boolType, nil, false},
		{"CyclicInitializers", "Cycle", boolType, nil, false},
		{"ExtraRequiredParameter", "Extra", uint8Type, nil, false},
		{"NoParameters", "Empty", uint8Type, nil, false},
		{"TrailingOptionalParameter", "Optional", uint8Type, nil, true},
		{"VariadicParameter", "Spread", uint8Type, nil, true},
		{"GenericParameter", "Generic", intType, nil, true},
		{"GenericIntegralFloat", "Generic", untypedFloat, constant.MakeFloat64(42), true},
		{"UnsatisfiedConstraint", "Generic", boolType, nil, false},
		{"InitializerRequiresTypeArgument", "Explicit", intType, nil, false},
		{"FunctionValueInitializer", "FunctionValue", uint8Type, nil, true},
		{"OverloadedInitializer", "Choice", stringType, nil, true},
		{"NoMatchingOverload", "Choice", boolType, nil, false},
		{"EmbeddedPointerParameter", "Wrapped", gotypes.NewPointer(typ("Embedded")), nil, true},
		{"UnrelatedEmbeddedPointer", "Wrapped", gotypes.NewPointer(typ("Recursive")), nil, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			value := gotypes.TypeAndValue{Type: tt.actual, Value: tt.value}
			assert.Equal(t, tt.want, CallValueTypeMatches(nil, value, typ(tt.target)))
		})
	}
}
