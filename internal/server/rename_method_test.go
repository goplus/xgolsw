package server

import (
	goast "go/ast"
	goparser "go/parser"
	gotypes "go/types"
	"strings"
	"testing"

	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentRenameMethodRelations(t *testing.T) {
	for _, kind := range []struct {
		name     string
		filename string
	}{
		{name: "XGo", filename: "main.xgo"},
		{name: "NormalClass", filename: "Record.gox"},
		{name: "ProjectClass", filename: "main_fixture.gox"},
		{name: "WorkClass", filename: "Worker_fixture.gox"},
	} {
		t.Run(kind.name, func(t *testing.T) {
			for _, tt := range []struct {
				name   string
				source string
			}{
				{name: "InterfaceAssignment", source: `type Reader interface { |Read() int }
type Other interface { |Read() int }
func run(reader Reader) {
var other Other = reader
println other.|Read()
}
`},
				{name: "InterfaceEmbedding", source: `type Reader interface { |Read() int }
type Other interface { Reader; |Read() int }
func run(reader Reader) {
var other Other = reader
println other.|Read()
}
`},
				{name: "Value", source: `type Reader interface { |Read() int }
type RecordValue struct{}
func (RecordValue) |Read() int { return 1 }
func run() {
var reader Reader = RecordValue{}
println reader.|Read()
println RecordValue{}.|Read()
}

`},
				{name: "Pointer", source: `type Reader interface { |Read() int }
type RecordValue struct{}
func (*RecordValue) |Read() int { return 1 }
func run() {
var reader Reader = new(RecordValue)
println reader.|Read()
println new(RecordValue).|Read()
}
`},
				{name: "Promoted", source: `type Reader interface { |Read() int }
type Base struct{}
func (Base) |Read() int { return 1 }
type RecordValue struct { Base }
func run() {
var reader Reader = RecordValue{}
println reader.|Read()
println RecordValue{}.|Read()
}
`},
				{name: "Anonymous", source: `type RecordValue struct{}
func (RecordValue) |Read() int { return 1 }
func run() {
var reader interface { |Read() int } = RecordValue{}
println reader.|Read()
}
`},
				{name: "Transitive", source: `type Reader interface { |Read() int }
type Closer interface { |Read() int; Close() }
type RecordValue struct{}
func (RecordValue) |Read() int { return 1 }
func (RecordValue) Close() {}
type Other struct{}
func (Other) |Read() int { return 2 }
func run() {
var reader Reader = Other{}
var closer Closer = RecordValue{}
println reader.|Read(), closer.|Read()
}
`},
				{name: "DifferentSignature", source: `type Reader interface { |Read() int }
type RecordValue struct{}
func (RecordValue) |Read() int { return 1 }
type Other struct{}
func (Other) Read() string { return "text" }
func run() {
var reader Reader = RecordValue{}
println reader.|Read(), Other{}.Read()
}
`},
			} {
				t.Run(tt.name, func(t *testing.T) {
					parts := strings.Split(tt.source, "|")
					source := strings.Join(parts, "")
					prefix := ""
					for _, part := range parts[:len(parts)-1] {
						prefix += part
						position := Position{Line: uint32(strings.Count(prefix, "\n")), Character: uint32(UTF16Len(prefix[strings.LastIndexByte(prefix, '\n')+1:]))}
						files := map[string][]byte{kind.filename: []byte(source)}
						if kind.name == "WorkClass" {
							files["main_fixture.gox"] = nil
						}
						s := newFrameworkTestServer(t, files)
						s.replier = newMockReplier()
						_, err := s.requestProject().TypeInfo()
						require.NoError(t, err)
						uri := s.toDocumentURI(kind.filename)
						edit, err := s.textDocumentRename(&RenameParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: position, NewName: "Fetch"})
						require.NoError(t, err)
						require.NotNil(t, edit)
						assert.Len(t, edit.Changes[uri], len(parts)-1)
						updated := applyResourceRenameTestEdits(t, source, edit.Changes[uri])
						assert.Equal(t, strings.ReplaceAll(tt.source, "|Read", "Fetch"), updated)
						s.ModifyFiles([]FileChange{{Path: kind.filename, Content: []byte(updated), Version: 1}})
						_, err = s.requestProject().TypeInfo()
						assert.NoError(t, err)
					}
				})
			}
		})
	}
}

func TestServerTextDocumentRenameImportedMethodRelation(t *testing.T) {
	for _, tt := range []struct {
		name   string
		source string
	}{
		{name: "ImportedInterface", source: `import "fmt"
type RecordValue struct{}
func (RecordValue) |String() string { return "" }
var value fmt.Stringer = RecordValue{}
println value.String()
`},
		{name: "ImportedImplementation", source: `import "bytes"
type Reader interface { |Read(p []byte) (int, error) }
var value Reader = new(bytes.Buffer)
`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, position := typeDisplayTestSource(t, tt.source)
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			s.replier = newMockReplier()
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			edit, err := s.textDocumentRename(&RenameParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: position, NewName: "Fetch"})
			assert.ErrorContains(t, err, "no editable project declaration")
			assert.Nil(t, edit)
		})
	}
}

func TestServerTextDocumentRenameImportedMethodContract(t *testing.T) {
	for _, tt := range []struct {
		name      string
		contract  string
		localType string
		use       string
	}{
		{name: "Parameter", contract: `func Accept(value interface { Read() int }) {}`, localType: "struct{}", use: "contract.Accept(RecordValue{})"},
		{name: "Callback", contract: `func Callback() func(interface { Read() int }) { return nil }`, localType: "struct{}", use: "contract.Callback()(RecordValue{})"},
		{name: "Field", contract: `type Options struct { Value interface { Read() int } }; func Accept(value Options) {}`, localType: "struct{}", use: "contract.Accept(contract.Options{Value: RecordValue{}})"},
		{name: "Slice", contract: `func Accept(value []interface { Read() int }) {}`, localType: "struct{}", use: "contract.Accept([]interface { Read() int }{RecordValue{}})"},
		{name: "MapKey", contract: `func Accept(value map[interface { Read() int }]int) {}`, localType: "struct{}", use: "contract.Accept(nil)"},
		{name: "MapValue", contract: `func Accept(value map[string]interface { Read() int }) {}`, localType: "struct{}", use: "contract.Accept(nil)"},
		{name: "Constraint", contract: `func Accept[T interface { ~int; Read() int }](value T) {}`, localType: "int", use: "contract.Accept(RecordValue(1))"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, position := typeDisplayTestSource(t, `import contract "example.com/contract"
type RecordValue `+tt.localType+`
func (RecordValue) |Read() int { return 1 }
func run() { `+tt.use+" }\n")
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			s.replier = newMockReplier()
			fset := token.NewFileSet()
			file, err := goparser.ParseFile(fset, "contract.go", "package contract\n"+tt.contract, 0)
			require.NoError(t, err)
			pkg, err := (&gotypes.Config{}).Check("example.com/contract", fset, []*goast.File{file}, nil)
			require.NoError(t, err)
			base := s.getProj().Importer
			s.getProj().Importer = testImporterFunc(func(path string) (*gotypes.Package, error) {
				if path == pkg.Path() {
					return pkg, nil
				}
				return base.Import(path)
			})
			_, err = s.requestProject().TypeInfo()
			require.NoError(t, err)
			edit, err := s.textDocumentRename(&RenameParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: position, NewName: "Fetch"})
			assert.ErrorContains(t, err, "no editable project declaration")
			assert.Nil(t, edit)
		})
	}
}

func TestServerTextDocumentRenameClassMethodContract(t *testing.T) {
	for _, kind := range []struct {
		name     string
		filename string
	}{
		{name: "NormalClass", filename: "Record.gox"},
		{name: "ProjectClass", filename: "main_fixture.gox"},
		{name: "WorkClass", filename: "Worker_fixture.gox"},
	} {
		t.Run(kind.name, func(t *testing.T) {
			const contract = `type Reader interface { Read() int }
func consume(reader Reader) int { return reader.Read() }
`
			const implementation = `func Read() int { return 1 }
func use() {
println consume(this)
println this.Read()
}
`
			for _, filename := range []string{"contract.xgo", kind.filename} {
				files := map[string][]byte{"contract.xgo": []byte(contract), kind.filename: []byte(implementation)}
				if kind.name == "WorkClass" {
					files["main_fixture.gox"] = nil
				}
				s := newFrameworkTestServer(t, files)
				s.replier = newMockReplier()
				_, err := s.requestProject().TypeInfo()
				require.NoError(t, err)
				params := &RenameParams{TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(filename)}, Position: Position{Character: uint32(strings.Index(string(files[filename]), "Read()"))}, NewName: "Fetch"}
				for version := range 2 {
					edit, err := s.textDocumentRename(params)
					require.NoError(t, err)
					require.NotNil(t, edit)
					require.Len(t, edit.Changes, 2)
					var changes []FileChange
					for _, path := range []string{"contract.xgo", kind.filename} {
						source := string(files[path])
						updated := applyResourceRenameTestEdits(t, source, edit.Changes[s.toDocumentURI(path)])
						assert.Equal(t, strings.ReplaceAll(source, "Read(", "Fetch("), updated)
						changes = append(changes, FileChange{Path: path, Content: []byte(updated), Version: version*2 + 1})
					}
					s.ModifyFiles(changes)
					_, err = s.requestProject().TypeInfo()
					require.NoError(t, err)
					s.ModifyFiles([]FileChange{{Path: "contract.xgo", Content: []byte(contract), Version: version*2 + 2}, {Path: kind.filename, Content: []byte(implementation), Version: version*2 + 2}})
				}
			}
		})
	}
}

func TestServerTextDocumentRenameMethodConflict(t *testing.T) {
	for _, tt := range []struct {
		name   string
		source string
	}{
		{name: "DirectMethod", source: `type Value struct{}
func (Value) |Read() int { return 1 }
func (Value) Fetch() {}
`},
		{name: "DirectField", source: "type Value struct{ Fetch int }\nfunc (Value) |Read() int { return 1 }\n"},
		{name: "InterfaceMethod", source: "type Reader interface { |Read() int; Fetch() }\n"},
		{name: "RelatedImplementation", source: `type Reader interface { |Read() int }
type Value struct{ Fetch int }
func (Value) Read() int { return 1 }
`},
		{name: "PromotedMethod", source: `type Base struct{}
func (Base) |Read() int { return 1 }
type Value struct { Base }
func (Value) Fetch() {}
func use(v Value) { println v.Read() }
`},
		{name: "PromotedField", source: `type Base struct{}
func (Base) |Read() int { return 1 }
type Value struct { Base; Fetch int }
func use(v Value) { println v.Read() }
`},
		{name: "AliasMethod", source: `type Value struct{}
func (Value) |Read() int { return 1 }
func (Value) fetch() string { return "text" }
var value int = Value{}.read
`},
		{name: "UnderscoreAliasMethod", source: `type Value struct{}
func (Value) |XGo_read() int { return 1 }
func (Value) fetch() string { return "text" }
var value int = Value{}._read
`},
		{name: "PromotedAliasField", source: `type Base struct{}
func (Base) |Read() int { return 1 }
type Value struct { Base; fetch string }
func use(v Value) { var n int = v.read; println n }
`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, position := typeDisplayTestSource(t, tt.source)
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			s.replier = newMockReplier()
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			edit, err := s.textDocumentRename(&RenameParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: position, NewName: "Fetch"})
			assert.ErrorContains(t, err, "conflicts")
			assert.Nil(t, edit)
		})
	}
}

func TestServerTextDocumentRenameKwargMethodRelation(t *testing.T) {
	const marked = `type Client struct{}
type Params interface { |SetCount(n int) Params }
type Options struct{}
func (Options) |SetCount(n int) Params { return nil }
func (Client) Params() Params { return nil }
func (Client) configure(params Params?) {}
func use(client Client) {
client.configure |setCount=1
println Options{}.|SetCount(2)
}

`
	parts := strings.Split(marked, "|")
	source := strings.Join(parts, "")
	var positions []Position
	prefix := ""
	for _, part := range parts[:len(parts)-1] {
		prefix += part
		positions = append(positions, Position{Line: uint32(strings.Count(prefix, "\n")), Character: uint32(UTF16Len(prefix[strings.LastIndexByte(prefix, '\n')+1:]))})
	}
	for _, target := range positions {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
		s.replier = newMockReplier()
		_, err := s.requestProject().TypeInfo()
		require.NoError(t, err)
		params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: target}
		var want []Location
		for _, position := range positions[2:] {
			end := position
			end.Character += 8
			want = append(want, Location{URI: params.TextDocument.URI, Range: Range{Start: position, End: end}})
		}
		got, err := s.textDocumentReferences(&ReferenceParams{TextDocumentPositionParams: params})
		require.NoError(t, err)
		assert.ElementsMatch(t, want, got)
		name := "SetLimit"
		if target == positions[2] {
			name = "setLimit"
		}
		edit, err := s.textDocumentRename(&RenameParams{TextDocument: params.TextDocument, Position: target, NewName: name})
		require.NoError(t, err)
		require.NotNil(t, edit)
		updated := applyResourceRenameTestEdits(t, source, edit.Changes[params.TextDocument.URI])
		assert.Equal(t, strings.NewReplacer("SetCount", "SetLimit", "setCount", "setLimit").Replace(source), updated)
		s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(updated), Version: 1}})
		_, err = s.requestProject().TypeInfo()
		assert.NoError(t, err)
	}
}

func TestServerTextDocumentRenameMethodAlias(t *testing.T) {
	t.Run("Underscore", func(t *testing.T) {
		for _, tt := range []struct {
			name        string
			position    Position
			newName     string
			declaration string
			alias       string
		}{
			{name: "Declaration", position: Position{Line: 1, Character: 19}, newName: "Fetch", declaration: "Fetch", alias: "fetch"},
			{name: "Alias", position: Position{Line: 3, Character: 21}, newName: "fetch", declaration: "Fetch", alias: "fetch"},
			{name: "AliasUnderscore", position: Position{Line: 3, Character: 21}, newName: "_fetch", declaration: "XGo_fetch", alias: "_fetch"},
			{name: "DeclarationUnderscore", position: Position{Line: 1, Character: 19}, newName: "XGo_fetch", declaration: "XGo_fetch", alias: "_fetch"},
			{name: "ExactName", position: Position{Line: 1, Character: 19}, newName: "local", declaration: "local", alias: "local()"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				const source = `type RecordValue struct{}
func (RecordValue) XGo_read() int { return 1 }
var item RecordValue
var value int = item._read
var other int = item.xGo_read
println item._read(), item.xGo_read(), item.XGo_read()
`
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
				s.replier = newMockReplier()
				_, err := s.requestProject().TypeInfo()
				require.NoError(t, err)
				const uri DocumentURI = "file:///main.xgo"
				edit, err := s.textDocumentRename(&RenameParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: tt.position, NewName: tt.newName})
				require.NoError(t, err)
				require.NotNil(t, edit)
				updated := applyResourceRenameTestEdits(t, source, edit.Changes[uri])
				assert.Contains(t, updated, "func (RecordValue) "+tt.declaration+"() int")
				assert.Contains(t, updated, "var value int = item."+tt.alias+"\n")
				assert.Contains(t, updated, "var other int = item."+tt.alias+"\n")
				s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(updated), Version: 1}})
				_, err = s.requestProject().TypeInfo()
				assert.NoError(t, err)
			})
		}
	})

	for _, tt := range []struct {
		name string
		use  string
	}{
		{name: "Call", use: "item.read()"},
		{name: "Property", use: "item.read"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := `type RecordValue struct{}
func (RecordValue) Read() int { return 1 }
var item RecordValue
var value int = ` + tt.use + `
println item.Read()
`
			for _, target := range []struct {
				name     string
				position Position
				newName  string
			}{
				{name: "Declaration", position: Position{Line: 1, Character: 19}, newName: "Fetch"},
				{name: "Alias", position: Position{Line: 3, Character: 21}, newName: "fetch"},
			} {
				t.Run(target.name, func(t *testing.T) {
					s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
					s.replier = newMockReplier()
					_, err := s.requestProject().TypeInfo()
					require.NoError(t, err)
					const uri DocumentURI = "file:///main.xgo"
					edit, err := s.textDocumentRename(&RenameParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: target.position, NewName: target.newName})
					require.NoError(t, err)
					require.NotNil(t, edit)
					updated := applyResourceRenameTestEdits(t, source, edit.Changes[uri])
					assert.Equal(t, strings.NewReplacer("Read", "Fetch", "read", "fetch").Replace(source), updated)
					s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(updated), Version: 1}})
					_, err = s.requestProject().TypeInfo()
					assert.NoError(t, err)
				})
			}
		})
	}
}

func TestServerTextDocumentRenameMethodAliasToExactName(t *testing.T) {
	for _, tt := range []struct {
		name    string
		newName string
	}{
		{name: "Unexported", newName: "fetch"},
		{name: "KeywordAlias", newName: "For"},
		{name: "Unicode", newName: "\u0393amma"},
		{name: "Underscore", newName: "_fetch"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const source = `type RecordValue struct{}
func (RecordValue) Read() int { return 1 }
var item RecordValue
var value int = item.read
println item.read(), item.Read()
`
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			s.replier = newMockReplier()
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			const uri DocumentURI = "file:///main.xgo"
			edit, err := s.textDocumentRename(&RenameParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: Position{Line: 1, Character: 19}, NewName: tt.newName})
			require.NoError(t, err)
			require.NotNil(t, edit)
			updated := applyResourceRenameTestEdits(t, source, edit.Changes[uri])
			want := `type RecordValue struct{}
func (RecordValue) ` + tt.newName + `() int { return 1 }
var item RecordValue
var value int = item.` + tt.newName + `()
println item.` + tt.newName + "(), item." + tt.newName + "()\n"
			assert.Equal(t, want, updated)
			s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(updated), Version: 1}})
			_, err = s.requestProject().TypeInfo()
			assert.NoError(t, err)
		})
	}
}

func TestServerTextDocumentRenameClassMethodAlias(t *testing.T) {
	for _, tt := range []struct {
		name string
		path string
	}{
		{name: "NormalClass", path: "Record.gox"},
		{name: "ProjectClass", path: "main_fixture.gox"},
		{name: "WorkClass", path: "Worker_fixture.gox"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const source = `func Read() int { return 1 }
func use() {
var a int = read
var b int = (this.read)
println a, b, read(), this.read(), (this.read)()
}
`
			files := map[string][]byte{tt.path: []byte(source)}
			if tt.name == "WorkClass" {
				files["main_fixture.gox"] = nil
			}
			s := newFrameworkTestServer(t, files)
			s.replier = newMockReplier()
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			uri := s.toDocumentURI(tt.path)
			edit, err := s.textDocumentRename(&RenameParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: Position{Character: 6}, NewName: "fetch"})
			require.NoError(t, err)
			require.NotNil(t, edit)
			updated := applyResourceRenameTestEdits(t, source, edit.Changes[uri])
			assert.Equal(t, `func fetch() int { return 1 }
func use() {
var a int = fetch()
var b int = (this.fetch())
println a, b, fetch(), this.fetch(), (this.fetch)()
}
`, updated)
			s.ModifyFiles([]FileChange{{Path: tt.path, Content: []byte(updated), Version: 1}})
			_, err = s.requestProject().TypeInfo()
			assert.NoError(t, err)
		})
	}
}

func TestServerTextDocumentRenameOverload(t *testing.T) {
	const source = `func readInt(n int) int { return n }
func readString(s string) int { return len(s) }
func Read = (
readInt
readString
)
println Read(1), Read("text"), readInt(2)
`
	for _, tt := range []struct {
		name     string
		position Position
		oldName  string
		newName  string
	}{
		{name: "WrapperDeclaration", position: Position{Line: 2, Character: 6}, oldName: "Read", newName: "Fetch"},
		{name: "WrapperCall", position: Position{Line: 6, Character: 9}, oldName: "Read", newName: "Fetch"},
		{name: "Candidate", position: Position{Line: 0, Character: 6}, oldName: "readInt", newName: "fetchInt"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			s.replier = newMockReplier()
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			const uri DocumentURI = "file:///main.xgo"
			params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: tt.position}
			var wantLocations []Location
			var wantHighlights []DocumentHighlight
			for offset := 0; offset < len(source); {
				index := strings.Index(source[offset:], tt.oldName)
				if index < 0 {
					break
				}
				start := offset + index
				prefix := source[:start]
				position := Position{Line: uint32(strings.Count(prefix, "\n")), Character: uint32(start - strings.LastIndexByte(prefix, '\n') - 1)}
				end := position
				end.Character += uint32(len(tt.oldName))
				rng := Range{Start: position, End: end}
				wantLocations = append(wantLocations, Location{URI: uri, Range: rng})
				kind := Read
				if len(wantHighlights) == 0 {
					kind = Write
				}
				wantHighlights = append(wantHighlights, DocumentHighlight{Range: rng, Kind: kind})
				offset = start + len(tt.oldName)
			}
			locations, err := s.textDocumentReferences(&ReferenceParams{TextDocumentPositionParams: params, Context: ReferenceContext{IncludeDeclaration: true}})
			require.NoError(t, err)
			assert.ElementsMatch(t, wantLocations, locations)
			highlights, err := s.textDocumentDocumentHighlight(&DocumentHighlightParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			require.NotNil(t, highlights)
			assert.ElementsMatch(t, wantHighlights, *highlights)
			if tt.name == "WrapperCall" {
				definition, err := s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: params})
				require.NoError(t, err)
				assert.Equal(t, Location{URI: uri, Range: Range{Start: Position{Character: 5}, End: Position{Character: 12}}}, definition)
			}
			edit, err := s.textDocumentRename(&RenameParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: tt.position, NewName: tt.newName})
			require.NoError(t, err)
			require.NotNil(t, edit)
			updated := applyResourceRenameTestEdits(t, source, edit.Changes[uri])
			assert.Equal(t, strings.ReplaceAll(source, tt.oldName, tt.newName), updated)
			s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(updated), Version: 1}})
			_, err = s.requestProject().TypeInfo()
			assert.NoError(t, err)
		})
	}
}

func TestServerTextDocumentRenameClassOverload(t *testing.T) {
	for _, tt := range []struct {
		name  string
		path  string
		class string
	}{
		{name: "NormalClass", path: "Record.gox", class: "Record"},
		{name: "ProjectClass", path: "main_fixture.gox", class: "App"},
		{name: "WorkClass", path: "Worker_fixture.gox", class: "Worker"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := `func readInt(n int) int { return n }
func readString(s string) int { return len(s) }
func (` + tt.class + `).Read = (
(` + tt.class + `).readInt
(` + tt.class + `).readString
)
func use() { println Read(1), read("text") }
`
			for _, position := range []Position{{Line: 2, Character: uint32(len(tt.class) + 8)}, {Line: 6, Character: 22}} {
				files := map[string][]byte{tt.path: []byte(source)}
				if tt.name == "WorkClass" {
					files["main_fixture.gox"] = nil
				}
				s := newFrameworkTestServer(t, files)
				s.replier = newMockReplier()
				_, err := s.requestProject().TypeInfo()
				require.NoError(t, err)
				uri := s.toDocumentURI(tt.path)
				edit, err := s.textDocumentRename(&RenameParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: position, NewName: "Fetch"})
				require.NoError(t, err)
				require.NotNil(t, edit)
				updated := applyResourceRenameTestEdits(t, source, edit.Changes[uri])
				assert.Equal(t, strings.NewReplacer("Read", "Fetch", "read(", "fetch(").Replace(source), updated)
				s.ModifyFiles([]FileChange{{Path: tt.path, Content: []byte(updated), Version: 1}})
				_, err = s.requestProject().TypeInfo()
				assert.NoError(t, err)
			}
		})
	}
}
