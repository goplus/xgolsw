package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	goast "go/ast"
	goparser "go/parser"
	gotypes "go/types"
	"strings"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/internal/pkgdata"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newNotRecommendedImportServer(t *testing.T, source string) *Server {
	t.Helper()

	s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
	proj := s.getProj()
	file, err := goparser.ParseFile(proj.Fset, "dependency.go", `package dependency
const XGoPackage = true
//xgols:notRecommended
var HiddenValue string
var VisibleValue string
var VisibleNumber int
//xgols:notRecommended
const HiddenLimit = "hidden"
const VisibleLimit = "visible"
const ByteLimit = 255
const BeyondByteLimit = 256
//xgols:notRecommended
type HiddenType struct{}
type Choice__0[T ~int] struct{}
//xgols:notRecommended
type Choice__1[T ~string] struct{}
type Record struct {
    //xgols:notRecommended
    HiddenField string
    VisibleField string
}
type AlphaReader = interface {
	// Read reads an alpha value.
	Read() int
}
type BetaReader[T any] = (interface {
	// Read reads a beta value.
	Read() T
})
type AliasReader = BetaReader[int]
type privateReader[T any] = interface {
	// Read reads a private value.
	Read() T
}
type concreteReader = privateReader[int]
type PublicReader = concreteReader
type hiddenReader interface {
    // Read reads an internal value.
    Read() int
}
type NamedReader hiddenReader
type readerImpl interface {
    // Read reads a promoted value.
    Read() int
}
type readerLayer interface { readerImpl }
type EmbeddedReader interface { readerLayer }
type privateValue[T any] = struct {
    // Value stores the payload.
    Value T
    //xgols:notRecommended
    Hidden T
}
type PublicValue = privateValue[int]
type PublicRecord privateValue[int]
type privateStats struct {
    // Value stores the payload.
    Value int
    //xgols:notRecommended
    Hidden int
}
type EmbeddedRecord struct { privateStats }
type pointerValue[T any] = struct {
    // Value stores the payload.
    Value T
    //xgols:notRecommended
    Hidden T
}
type privatePointer = *pointerValue[int]
type PublicPointer = privatePointer
type InlinePointer = *struct {
    // Value stores the payload.
    Value int
    //xgols:notRecommended
    Hidden int
}
type privateCounter[T any] struct { Value T }
// Read returns the current count.
func (counter privateCounter[T]) Read() T { return counter.Value }
// ReadLegacy returns the count through the legacy API.
//xgols:notRecommended
func (counter privateCounter[T]) ReadLegacy() T { return counter.Value }
type PublicCounter = privateCounter[int]
type privateResettable[T any] struct { Value T }
// Reset clears the current count.
func (counter *privateResettable[T]) Reset() { var zero T; counter.Value = zero }
type resettablePointer = *privateResettable[int]
type PublicCounterPointer = resettablePointer
//xgols:notRecommended
func HiddenText() string { return "hidden" }
func VisibleText() string { return "visible" }
//xgols:notRecommended
func (Record) HiddenMethod() string { return "hidden" }
func (Record) VisibleMethod() string { return "visible" }
func Put2__0(value int) {}
func PutString(value string) {}
//xgols:notRecommended
const XGoo__Put2 = ",PutString"
const XGoo_PutAlternative = "Put2__0"
func Pick__0(value int) {}
//xgols:notRecommended
const XGoo_Pick = "Missing"
const (
    // Pick selects a conversion.
    XGoo__Pick = ",Pick__1"
)
// Pick__1 handles text.
func Pick__1(value string) {}
func Pick__2(flag bool) bool { return flag }
//xgols:notRecommended
const XGoo_Chained = "Target"
const XGoo_Target = "Pick__0,Pick__1"
const XGoo_chained = "Target"
// Chained delegates to Target.
const XGoo__Chained = "Target"
func Record_Run2__0(value int) {}
func (Record) Run2__0(value int) {}
func (Record) RunString(value string) {}
//xgols:notRecommended
const XGoo_Record_Run2 = ",.RunString"
func GatherInt(record Record, value int) {}
func GatherString(record Record, value string) {}
//xgols:notRecommended
const XGoo__XGot_Record_Gather = "GatherInt,GatherString"
func DecideInt(value int) {}
func DecideString(value string) {}
//xgols:notRecommended
const XGoo__XGos_Record_Decide = "DecideInt,DecideString"
type Record_Name struct{}
func (Record_Name) MoveInt(value int) {}
func (Record_Name) MoveString(value string) {}
//xgols:notRecommended
const XGoo__Record_Name__Move_Fast = ".MoveInt,.MoveString"
func TransformInt[T ~int](value T) T { return value }
func TransformString[T ~string](value T) T { return value }
// Transform converts a value.
//xgols:notRecommended
const XGoo__XGox_Transform = "TransformInt,TransformString"
func XGox_TypeConvert[T any](value T) T { return value }
// ConvertValue applies the requested type to a value.
const XGoo_ConvertValue = "TypeConvert"
// ConvertEither accepts either an inferred or an explicit type.
const XGoo_ConvertEither = "XGox_TypeConvert,TypeConvert"
// ForwardConvert delegates to ConvertEither.
const XGoo_ForwardConvert = "ConvertEither"
const XGoo__ZTransform = "Transform"
func ConvertPair(first, second int) int { return second }
const XGoo_ConvertPairOrType = "TypeConvert,ConvertPair"
func XGox_CompareType[T comparable](value int) int { return value }
const XGoo_CompareEither = "CompareType,ConvertPair"
func XGox_PickSlice[S ~[]E, E any](value E) E { return value }
const XGoo_PickSliceOrPair = "PickSlice,ConvertPair"
func XGox_ConvertSlice[T any](values []T) []T { return values }
func XGox_ConvertText[T any](text string) T { var zero T; return zero }
const XGoo_ConvertByType = "ConvertSlice,TypeConvert,ConvertText"
func XGox_KeepFirst[A, B any](first A, second B) A { return first }
const XGoo_KeepFirstOrPair = "KeepFirst,ConvertPair"
func XGox_KeepSame[A, B any](first, second B) B { return first }
func XGox_KeepWithOptions[A, B any](first B, options map[string]B) B { return first }
const XGoo_KeepSameOrPair = "KeepSame,ConvertPair"
func XGox_KeepSlice[A any, S ~[]A](values S) S { return values }
const XGoo_KeepSliceOrPair = "KeepSlice,ConvertPair"
func XGox_JoinSlices[A any, S ~[]A](first, second S) S { return append(first, second...) }
const XGoo_JoinSlicesOrPair = "JoinSlices,ConvertPair"
func XGox_CollectTail[A any, B ~[]A](first A, tail ...B) []B { return tail }
func XGox_CollectLabelledTail[A any, B ~[]A](first A, label string, tail ...B) []B { return tail }
const XGoo_CollectTailOrLabelled = "CollectTail,CollectLabelledTail"
func XGox_CompareSlices[A any, S ~[]A](first S, others ...S) S { return first }
const XGoo_CompareSlicesOrPair = "CompareSlices,ConvertPair"
func CurrentText() string { return "current" }
func CurrentPrefixed(prefix string) string { return prefix }
//xgols:notRecommended
const XGoo_Current = "CurrentText,CurrentPrefixed"
var LookupPrefixed = func(prefix string) string { return prefix }
const XGoo_Lookup = "LookupPrefixed,CurrentText"
const XGoo_Search = "Lookup"
const XGoo__XGo_Status = "Lookup"
var StoredText = func() string { return "stored" }
const XGoo_Stored = "StoredText"
// VariableLookup resolves a stored value.
const XGoo__VariableLookup = "Stored"
// ReadTextCallback reads a shared label.
var ReadTextCallback = func() string { return "text" }
const XGoo_ReadShared = "ReadTextCallback"
const XGoo_ReadWithType = "TypeConvert,ReadTextCallback"
//xgols:notRecommended
var HiddenStoredText = func() string { return "hidden" }
const XGoo_HiddenStored = "HiddenStoredText"
const XGoo__HiddenVariableLookup = "HiddenStored"
const XGoo_HiddenLookup = "HiddenText"
const XGoo_HiddenSearch = "HiddenLookup"
func (Record) MetricText() string { return "metric" }
func (Record) MetricPrefixed(prefix string) string { return prefix }
//xgols:notRecommended
const XGoo_Record_Metric = ".MetricText,.MetricPrefixed"
const XGoo_Record_Find = ".MetricText"
// Result reports the metric for this record.
const XGoo_Record_Result = ".Find"
const XGoo_Record_HiddenFind = ".HiddenMethod"
const XGoo_Record_HiddenResult = ".HiddenFind"
func (Record) FormatNumber(value int) {}
func (Record) Format__0(flag bool) bool { return flag }
// Format documents a method overload.
const XGoo_Record_Format = ".FormatNumber"
// Render uses the formatting overloads.
const XGoo_Record_Render = ".Format"
func ListRecord(record Record, value int) {}
// List documents a template overload.
const XGoo__XGot_Record_List = "ListRecord"
type CollectingRecord struct { Collect int; Record }
func CollectRecord(record any, value int) {}
const XGoo_ListBase = "CollectRecord"
// Collect documents a chained template overload.
const XGoo__XGot_Record_Collect = "ListBase"
func AdaptRecord[T any](record Record, value T) T { return value }
// Adapt documents a template overload with type arguments.
const XGoo__XGot_Record_XGox_Adapt = "AdaptRecord"
func XGox_ConvertRecord[T any](record Record, value T) T { return value }
const XGoo_RecordConvertBase = "ConvertRecord"
// RelayType delegates to a conversion with an explicit type argument.
const XGoo__XGot_Record_RelayType = "RecordConvertBase"
func ReadRecord(record Record) string { return "record" }
const XGoo__XGot_Record_RelayValue = "ReadRecord,ConvertRecord"
// XGot_Record_ApplyVariable applies a stored callback.
var XGot_Record_ApplyVariable = func(record Record, value int) int { return value }
const XGoo_ApplyCallback = "XGot_Record_ApplyVariable"
const XGoo__XGot_Record_InvokeVariable = "ApplyCallback"
const XGoo__XGos_Record_ApplyStatic = "XGot_Record_ApplyVariable"
func ApplyPairValues(first string, second int) int { return second }
func ApplyFlagValues(first int, second bool) int { return first }
func ApplyTextValue(value string) int { return len(value) }
const XGoo_ApplyPair = "ApplyPairValues"
const XGoo_ApplyPairs = "ApplyPairValues,ApplyFlagValues"
const XGoo_ApplyValues = "ApplyPairValues,ApplyTextValue"
func ApplyOptionalValues(first string, __xgo_optional_value int) int { return __xgo_optional_value }
func ApplyClosurePair(first string, __xgo_autoclosure_value func() int) int { return __xgo_autoclosure_value() }
func ApplyVariadicValues(first string, values ...int) int { return len(values) }
func ApplyAnyValues(values ...any) int { return len(values) }
func ApplySamePair[T any](first, second T) T { return first }
func ApplySameValues[T ~int](values ...T) int { return len(values) }
func XGot_Record_ApplyTextPair(record Record, first string, second int) int { return second }
func ProvidePair() (string, int) { return "", 1 }
//xgols:notRecommended
func ProvideHiddenPair() (string, int) { return "", 1 }
func ProvideWrongPair() (string, string) { return "", "" }
func ProvideSingle() string { return "" }
// XGot_Record_ReadVariable reads the record's value.
var XGot_Record_ReadVariable = func(record Record) int { return 1 }
func NewRecord(value int) Record { return Record{} }
// Make documents a static overload.
const XGoo__XGos_Record_Make = "NewRecord"
const XGoo_NewBase = "NewRecord"
// Build documents a chained static overload.
const XGoo__XGos_Record_Build = "NewBase"
`, goparser.ParseComments)
	require.NoError(t, err)
	pkg, err := new(gotypes.Config).Check("example.com/dependency", proj.Fset, []*goast.File{file}, nil)
	require.NoError(t, err)
	doc := pkgdoc.NewGo(pkg.Path(), &goast.Package{Name: pkg.Name(), Files: map[string]*goast.File{"dependency.go": file}})
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	entry, err := writer.Create(pkg.Path() + ".pkgdoc")
	require.NoError(t, err)
	require.NoError(t, json.NewEncoder(entry).Encode(doc))
	require.NoError(t, writer.Close())
	data, err := pkgdata.New(archive.Bytes())
	require.NoError(t, err)
	baseImporter, baseLookup := proj.Importer, s.lookupPkgDoc
	proj.Importer = testImporterFunc(func(path string) (*gotypes.Package, error) {
		if path == pkg.Path() {
			return pkg, nil
		}
		return baseImporter.Import(path)
	})
	s.lookupPkgDoc = func(path string) (*pkgdoc.PkgDoc, error) {
		if path == pkg.Path() {
			return data.GetPkgDoc(path)
		}
		return baseLookup(path)
	}
	return s
}

func TestServerNotRecommendedImportedDeclarations(t *testing.T) {
	for _, tt := range []struct {
		name, source string
		want, absent []string
	}{
		{
			name:   "PackageMembers",
			source: "import dep \"example.com/dependency\"\nfunc run() { dep.|VisibleText() }\n",
			want:   []string{"VisibleValue", "VisibleLimit", "Record", "visibleText"},
			absent: []string{"HiddenValue", "HiddenLimit", "HiddenType", "hiddenText", "HiddenText"},
		},
		{
			name:   "DotImport",
			source: "import . \"example.com/dependency\"\nfunc run() {\n|\n}\n",
			want:   []string{"VisibleValue", "VisibleLimit", "Record", "visibleText"},
			absent: []string{"HiddenValue", "HiddenLimit", "HiddenType", "hiddenText", "HiddenText"},
		},
		{
			name:   "Members",
			source: "import dep \"example.com/dependency\"\nvar record dep.Record\nfunc run() { println record.|visibleMethod }\n",
			want:   []string{"VisibleField", "visibleMethod"},
			absent: []string{"HiddenField", "hiddenMethod", "HiddenMethod"},
		},
		{
			name:   "EncodedPackageOverload",
			source: "import dep \"example.com/dependency\"\nfunc run() { dep.|put2(1) }\n",
			want:   []string{"putString", "putAlternative", "record_Run2", "pick"},
			absent: []string{"put2", "current"},
		},
		{
			name:   "EncodedDotImportOverload",
			source: "import . \"example.com/dependency\"\nfunc run() {\n|\nput2 1\n}\n",
			want:   []string{"putString"},
			absent: []string{"put2", "Put2"},
		},
		{
			name:   "EncodedMethodOverload",
			source: "import dep \"example.com/dependency\"\nvar record dep.Record\nfunc run() { record.|run2(1) }\n",
			want:   []string{"runString"},
			absent: []string{"run2"},
		},
		{
			name:   "EncodedTemplateOverload",
			source: "import dep \"example.com/dependency\"\nvar record dep.Record\nfunc run() { record.|gather(1) }\n",
			want:   []string{"visibleMethod"},
			absent: []string{"gather"},
		},
		{
			name:   "EncodedStaticOverload",
			source: "import dep \"example.com/dependency\"\nfunc run() { dep.Record.|decide(1) }\n",
			absent: []string{"decide", "Decide"},
		},
		{
			name:   "EscapedMethodOverload",
			source: "import dep \"example.com/dependency\"\nvar record dep.Record_Name\nfunc run() { record.|move_Fast(1) }\n",
			want:   []string{"moveInt", "moveString"},
			absent: []string{"move_Fast"},
		},
		{
			name:   "TypeAsParametersOverload",
			source: "import dep \"example.com/dependency\"\nfunc run() { _ = dep.|transform(int, 1) }\n",
			want:   []string{"transformInt"},
			absent: []string{"transform", "xGox_Transform"},
		},
		{
			name:   "MarkedTypeOverload",
			source: "import dep \"example.com/dependency\"\nvar choice dep.Ch|oice[string]\n",
			absent: []string{"Choice", "Choice__1"},
		},
		{
			name:   "UnmarkedTypeOverload",
			source: "import dep \"example.com/dependency\"\nvar choice dep.Ch|oice[int]\n",
			want:   []string{"Choice"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, tt.source)
			s := newNotRecommendedImportServer(t, source)
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
			for _, name := range tt.want {
				assert.Contains(t, labels, name)
			}
			for _, name := range tt.absent {
				assert.NotContains(t, labels, name)
			}
		})
	}
}

func TestServerNotRecommendedImportedInputNames(t *testing.T) {
	s := newNotRecommendedImportServer(t, `import . "example.com/dependency"
func Use(value string) {}
func UseByteCount(value uint8) {}
use "value"
useByteCount 1
`)
	t.Cleanup(s.Close)
	useExportedDependency(t, s, "example.com/dependency")
	_, err := s.requestProject().TypeInfo()
	require.NoError(t, err)
	slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}})
	require.NoError(t, err)
	require.Len(t, slots, 2)
	names := slots[0].PredefinedNames
	for _, name := range []string{"HiddenValue", "HiddenLimit", "VisibleNumber", "hiddenText", "current", "hiddenSearch", "_Status", "xGo_Status",
		"hiddenStored", "hiddenVariableLookup", "hiddenStoredText"} {
		assert.NotContains(t, names, name)
	}
	for _, name := range []string{"VisibleValue", "VisibleLimit", "visibleText", "currentText", "search", "variableLookup"} {
		assert.Contains(t, names, name)
	}
	assert.Contains(t, slots[1].PredefinedNames, "ByteLimit")
	assert.NotContains(t, slots[1].PredefinedNames, "BeyondByteLimit")
	assert.NotContains(t, slots[1].PredefinedNames, "VisibleLimit")
}

func TestServerCompletionImportedConstantBounds(t *testing.T) {
	source, pos := typeDisplayTestSource(t, `import . "example.com/dependency"
func Use(value uint8) {}
use |ByteLimit
`)
	s := newNotRecommendedImportServer(t, source)
	t.Cleanup(s.Close)
	useExportedDependency(t, s, "example.com/dependency")
	_, err := s.requestProject().TypeInfo()
	require.NoError(t, err)
	labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
	assert.Contains(t, labels, "ByteLimit")
	assert.NotContains(t, labels, "BeyondByteLimit")

	s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(strings.Replace(source, "use ByteLimit", "use BeyondByteLimit", 1)), Version: 2}})
	_, err = s.requestProject().TypeInfo()
	require.ErrorContains(t, err, "BeyondByteLimit")
}

func TestServerImportedOverloadCompletionUsesListedImplementations(t *testing.T) {
	const namedImport = "import dep \"example.com/dependency\"\n"
	for _, tt := range []struct {
		name, source, label, overview string
		wantCount                     int
	}{
		{name: "Package", source: namedImport + "dep.|pick(1)\n", label: "pick", wantCount: 2},
		{name: "DotImport", source: "import . \"example.com/dependency\"\n|pick(1)\n", label: "pick", wantCount: 2},
		{name: "Chained", source: namedImport + "dep.|chained(1)\n", label: "chained", wantCount: 2},
		{name: "Method", source: namedImport + "var record dep.Record\nrecord.|format(1)\n", label: "format", wantCount: 1},
		{name: "FunctionValue", source: namedImport + "var f func(bool) bool = dep.Pi|ck__2\n", label: "Pick__2",
			overview: "Pick__2(flag bool) bool", wantCount: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, tt.source)
			s := newNotRecommendedImportServer(t, source)
			t.Cleanup(s.Close)
			requireNoDiagnostics(t, s)
			var matches []CompletionItem
			for _, item := range completionItemsAt(t, s, "main.xgo", pos) {
				if item.Label == tt.label {
					matches = append(matches, item)
				}
			}
			assert.Len(t, matches, tt.wantCount)
			if tt.overview != "" {
				require.Len(t, matches, 1)
				require.NotNil(t, matches[0].Documentation)
				doc := requireValueAs[MarkupContent](t, matches[0].Documentation.Value)
				assert.Contains(t, doc.Value, `overview="func `+tt.overview+`"`)
			}
		})
	}
}

func TestServerNotRecommendedImportedProperties(t *testing.T) {
	s := newNotRecommendedImportServer(t, `import dep "example.com/dependency"
type Local struct { dep.Record }
var record Local
println record.metric
`)
	_, err := s.requestProject().TypeInfo()
	require.NoError(t, err)
	for _, exclude := range []bool{false, true} {
		properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: "Local", ExcludeNotRecommended: exclude})
		require.NoError(t, err)
		var names []string
		for _, property := range properties {
			names = append(names, property.Name)
		}
		assert.Contains(t, names, "metricText")
		if exclude {
			assert.NotContains(t, names, "metric")
			assert.NotContains(t, names, "hiddenResult")
		} else {
			assert.Contains(t, names, "metric")
			assert.Contains(t, names, "hiddenResult")
		}
	}
}

func TestServerImportedNestedAutoProperties(t *testing.T) {
	const imports = "import dep \"example.com/dependency\"\n"
	for _, tt := range []struct {
		name, source, label, unwantedLabel string
	}{
		{name: "Package", source: imports + "var value string = dep.|search\n", label: "search", unwantedLabel: "readWithType"},
		{name: "PackageUnderscore", source: imports + "var value string = dep.|xGo_Status\n",
			label: "xGo_Status", unwantedLabel: "_Status"},
		{name: "FunctionVariable", source: imports + "var value string = dep.|variableLookup\n",
			label: "variableLookup", unwantedLabel: "hiddenVariableLookup"},
		{name: "Method", source: imports + "var record dep.Record\nvar value string = record.|result\n", label: "result"},
		{name: "TemplateWithTypeArguments", source: imports + "var record dep.Record\nvar value string = record.|relayValue\n",
			label: "relayValue", unwantedLabel: "relayType"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, tt.source)
			s := newNotRecommendedImportServer(t, source)
			t.Cleanup(s.Close)
			requireNoDiagnostics(t, s)

			proj := s.requestProject()
			info, err := expressionTypeInfo(proj)
			require.NoError(t, err)
			file, err := proj.ASTFile("main.xgo")
			require.NoError(t, err)
			var property *ast.SelectorExpr
			ast.Inspect(file, func(node ast.Node) bool {
				if selector, ok := node.(*ast.SelectorExpr); ok && selector.Sel.Name == tt.label {
					property = selector
				}
				return true
			})
			require.NotNil(t, property)
			assert.Same(t, gotypes.Typ[gotypes.String], info.TypeOf(property))
			labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
			assert.Contains(t, labels, tt.label)
			if tt.unwantedLabel != "" {
				assert.NotContains(t, labels, tt.unwantedLabel)
			}
		})
	}
}

func TestServerNotRecommendedDeclarationAttachment(t *testing.T) {
	source, pos := typeDisplayTestSource(t, `//xgols:notRecommended
var (
    groupFirst, groupSecond int
    groupThird int
)
var (
    //xgols:notRecommended
    specFirst, specSecond int
    visibleValue int
)
//xgols:notRecommended
const (
    groupLimit = 1
    groupOtherLimit = 2
)
//xgols:notRecommended
type (
    GroupType struct{}
    OtherGroupType struct{}
)
//xgols:notRecommended

var detached int
var trailing int //xgols:notRecommended
/*xgols:notRecommended*/
var block int
// xgols:notRecommended
var spaced int
//xgols:not-recommended
var dashed int
//xgols:notRecommended because internal
var arguments int
//xgols:notRecommended
func HiddenOuter() {
    var nested int
    println nested
}
func run() {
|
}
`)
	s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
	_, err := s.requestProject().TypeInfo()
	require.NoError(t, err)
	labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
	for _, name := range []string{"groupFirst", "groupSecond", "groupThird", "specFirst", "specSecond", "groupLimit", "groupOtherLimit", "GroupType", "OtherGroupType", "hiddenOuter", "HiddenOuter"} {
		assert.NotContains(t, labels, name)
	}
	for _, name := range []string{"visibleValue", "detached", "trailing", "block", "spaced", "dashed", "arguments"} {
		assert.Contains(t, labels, name)
	}
}

func TestServerNotRecommendedEmbeddedField(t *testing.T) {
	source, pos := typeDisplayTestSource(t, `type Embedded struct { Value int }
//xgols:notRecommended
type Record struct {
    //xgols:notRecommended
    *Embedded
    Visible int
}
var record Record
func run() {

println record.|Visible
}
`)
	s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
	t.Cleanup(s.Close)
	_, err := s.requestProject().TypeInfo()
	require.NoError(t, err)
	labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
	assert.NotContains(t, labels, "Embedded")
	assert.Contains(t, labels, "Visible")
	assert.Contains(t, labels, "Value")
	_, typePos := typeDisplayTestSource(t, `type Embedded struct { Value int }
//xgols:notRecommended
type Record struct {
    //xgols:notRecommended
    *Embedded
    Visible int
}
var record Record
func run() {
|
println record.Visible
}
`)
	labels = completionItemLabels(completionItemsAt(t, s, "main.xgo", typePos))
	assert.Contains(t, labels, "Embedded")
	assert.NotContains(t, labels, "Record")
}
