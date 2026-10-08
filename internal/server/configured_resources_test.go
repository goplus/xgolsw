package server

import (
	"encoding/json"
	gotypes "go/types"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/goplus/xgolsw/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const resourceTestConfig = `{"dataFile":"resources.json","types":[
{"pkgPath":"example.com/support/v2","typeName":"Name","contextURI":"demo://resources/clips"},
{"pkgPath":"example.com/alternate","typeName":"Name","contextURI":"demo://resources/scenes"}
]}`

func newConfiguredResourceTestServer(t *testing.T, files map[string][]byte, configuration string) *Server {
	t.Helper()
	base := newImportTestServer(t, files)
	resources, err := config.ParseResourceConfig([]byte(configuration))
	require.NoError(t, err)
	return New(base.getProj(), newMockReplier(), base.fileMapGetter, &MockScheduler{}, base.listPkgs, base.lookupPkgDoc, resources)
}

func TestServerConfiguredResources(t *testing.T) {
	const filename = "main_fixture.gox"
	const uri = DocumentURI("file:///" + filename)
	source, pos := typeDisplayTestSource(t, `import clips "example.com/support/v2"
import scenes "example.com/alternate"
clips.Accept "In|tro"
scenes.Accept "Intro"
clips.Accept "Missing"
var plain = "Intro"
`)
	s := newConfiguredResourceTestServer(t, map[string][]byte{
		filename:         []byte(source),
		"resources.json": []byte(`{"demo://resources/clips":["Intro","Finale"],"demo://resources/scenes":["Intro","Studio"]}`),
	}, resourceTestConfig)
	proj := s.requestProject()
	_, err := proj.TypeInfo()
	require.NoError(t, err)
	links, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: TextDocumentIdentifier{URI: uri}})
	require.NoError(t, err)
	var targets []string
	for _, link := range links {
		if link.Target != nil && strings.HasPrefix(string(*link.Target), "demo:") {
			targets = append(targets, string(*link.Target))
		}
	}
	assert.ElementsMatch(t, []string{"demo://resources/clips/Intro", "demo://resources/scenes/Intro"}, targets)
	hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: pos}})
	require.NoError(t, err)
	require.NotNil(t, hover)
	assert.Contains(t, hover.Contents.Value, "demo://resources/clips/Intro")
	labels := completionItemLabels(completionItemsAt(t, s, filename, pos))
	assert.Contains(t, labels, "Intro")
	assert.Contains(t, labels, "Finale")
	assert.NotContains(t, labels, "Studio")
	slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: uri}}})
	require.NoError(t, err)
	var values []any
	for _, slot := range slots {
		if slot.Input.Type == XGoInputTypeResourceName {
			require.NotNil(t, slot.Accept.ResourceContext)
			values = append(values, slot.Input.Value)
		}
	}
	assert.ElementsMatch(t, []any{XGoResourceURI("demo://resources/clips/Intro"), XGoResourceURI("demo://resources/scenes/Intro"), XGoResourceURI("demo://resources/clips/Missing")}, values)
	diagnostics, err := s.diagnosticsAt(proj)
	require.NoError(t, err)
	assert.True(t, slices.ContainsFunc(diagnostics.diagnostics[uri], func(diagnostic Diagnostic) bool {
		return diagnostic.Message == `resource "Missing" not found in "demo://resources/clips"`
	}))
	analysis, err := analyzeFramework(proj)
	require.NoError(t, err)
	again, err := analyzeFramework(proj.Snapshot())
	require.NoError(t, err)
	assert.Same(t, analysis, again)
}

func TestServerConfiguredResourceRename(t *testing.T) {
	const source = `import clips "example.com/support/v2"
import scenes "example.com/alternate"
const name = "Intro"
clips.Accept name
scenes.Accept name
var plain = name
`
	s := newConfiguredResourceTestServer(t, map[string][]byte{
		"main.xgo": []byte(source), "resources.json": []byte(`{"demo://resources/clips":["Intro"],"demo://resources/scenes":["Intro"]}`),
	}, resourceTestConfig)
	edit, err := s.renameResources([]XGoRenameResourceParams{
		{Resource: XGoResourceIdentifier{URI: "demo://resources/clips/Intro"}, NewName: "Next/Clip"},
		{Resource: XGoResourceIdentifier{URI: "demo://resources/scenes/Intro"}, NewName: "New\"Scene"},
	})
	require.NoError(t, err)
	require.NotNil(t, edit)
	updated := applyResourceRenameTestEdits(t, source, edit.Changes["file:///main.xgo"])
	assert.Contains(t, updated, `const name = "Intro"`)
	assert.Contains(t, updated, `clips.Accept "Next/Clip"`)
	assert.Contains(t, updated, `scenes.Accept "New\"Scene"`)
	assert.Contains(t, updated, `var plain = name`)
	for _, tt := range []struct {
		name   string
		params []XGoRenameResourceParams
	}{
		{"UnknownCollection", []XGoRenameResourceParams{{Resource: XGoResourceIdentifier{URI: "other://resources/Intro"}, NewName: "Next"}}},
		{"MalformedURI", []XGoRenameResourceParams{{Resource: XGoResourceIdentifier{URI: "demo://resources/clips/%"}, NewName: "Next"}}},
		{"EmptyName", []XGoRenameResourceParams{{Resource: XGoResourceIdentifier{URI: "demo://resources/clips/Intro"}}}},
		{"DuplicateSource", []XGoRenameResourceParams{{Resource: XGoResourceIdentifier{URI: "demo://resources/clips/Intro"}, NewName: "Next"}, {Resource: XGoResourceIdentifier{URI: "demo://resources/clips/Intro"}, NewName: "Other"}}},
		{"ExistingTarget", []XGoRenameResourceParams{{Resource: XGoResourceIdentifier{URI: "demo://resources/clips/Other"}, NewName: "Intro"}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newConfiguredResourceTestServer(t, map[string][]byte{
				"main.xgo": []byte(source), "resources.json": []byte(`{"demo://resources/clips":["Intro"],"demo://resources/scenes":["Intro"]}`),
			}, resourceTestConfig)
			edit, err := s.renameResources(tt.params)
			assert.Error(t, err)
			assert.Nil(t, edit)
		})
	}
}

func TestServerConfiguredResourceState(t *testing.T) {
	const source = "import clips \"example.com/support/v2\"\nclips.Accept \"Intro\"\n"
	newServer := func(t *testing.T) *Server {
		t.Helper()
		return newConfiguredResourceTestServer(t, map[string][]byte{"main.xgo": []byte(source), "resources.json": []byte(`{"demo://resources/clips":["Intro"]}`)}, resourceTestConfig)
	}
	t.Run("ConcurrentColdCache", func(t *testing.T) {
		s := newServer(t)
		proj := s.requestProject()
		var analyses [8]*frameworkAnalysis
		var errs [8]error
		var wg sync.WaitGroup
		for i := range analyses {
			wg.Go(func() { analyses[i], errs[i] = analyzeFramework(proj) })
		}
		wg.Wait()
		for i := range analyses {
			require.NoError(t, errs[i])
			require.NotNil(t, analyses[i])
			assert.Same(t, analyses[0], analyses[i])
		}
	})
	for _, tt := range []struct{ name, data string }{
		{"Removed", `{}`},
		{"Malformed", `{`},
		{"Restored", `{"demo://resources/clips":["Intro"]}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newServer(t)
			old := s.requestProject()
			previous, err := analyzeFramework(old)
			require.NoError(t, err)
			s.ModifyFiles([]FileChange{{Path: "resources.json", Content: []byte(`{`), Version: 1}})
			_, err = analyzeFramework(s.requestProject())
			require.NoError(t, err)
			s.ModifyFiles([]FileChange{{Path: "resources.json", Content: []byte(tt.data), Version: 2}})
			proj := s.requestProject()
			current, err := analyzeFramework(proj)
			require.NoError(t, err)
			assert.NotSame(t, previous, current)
			links, err := documentLinksForResources(proj, "main.xgo")
			require.NoError(t, err)
			if tt.name == "Restored" {
				assert.Len(t, links, 1)
			} else {
				assert.Empty(t, links)
			}
			oldLinks, err := documentLinksForResources(old, "main.xgo")
			require.NoError(t, err)
			assert.Len(t, oldLinks, 1)
		})
	}
}

func TestServerConfiguredResourceValidation(t *testing.T) {
	for _, tt := range []struct{ name, source, typeName, manifest, message string }{
		{"MissingType", "var count int\n", "Asset", `{}`, "not found"},
		{"NonString", "type Asset int\n", "Asset", `{}`, "underlying type string"},
		{"UnknownCollection", "type Asset string\n", "Asset", `{"other://assets":[]}`, "unknown resource collection"},
		{"NullManifest", "type Asset string\n", "Asset", `null`, "must be an object"},
		{"EmptyResource", "type Asset string\n", "Asset", `{"demo://assets":[""]}`, "invalid resource name"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			configuration, err := json.Marshal(map[string]any{"dataFile": "resources.json", "types": []config.ResourceType{{PkgPath: "main", TypeName: tt.typeName, ContextURI: "demo://assets"}}})
			require.NoError(t, err)
			s := newConfiguredResourceTestServer(t, map[string][]byte{"main.xgo": []byte(tt.source), "resources.json": []byte(tt.manifest)}, string(configuration))
			analysis, err := analyzeFramework(s.requestProject())
			require.NoError(t, err)
			require.NotNil(t, analysis)
			require.NotEmpty(t, analysis.resources.diagnostics)
			assert.Contains(t, analysis.resources.diagnostics[0].diagnostic.Message, tt.message)
		})
	}
}

func newMixedResourceTestServer(t *testing.T, source string) *Server {
	t.Helper()
	base := newSpxTestServer(t, map[string][]byte{
		"main.spx":                       []byte("import sdk \"github.com/goplus/spx/v3\"\ntype Clip string\nfunc Use(name Clip) {}\n" + source),
		"assets/index.json":              []byte(`{}`),
		"assets/sounds/Intro/index.json": []byte(`{}`),
		"assets/sounds/Beep/index.json":  []byte(`{}`),
		"resources.json":                 []byte(`{"demo://clips":["Intro","Finale"]}`),
	})
	resources, err := config.ParseResourceConfig([]byte(`{"dataFile":"resources.json","types":[{"pkgPath":"main","typeName":"Clip","contextURI":"demo://clips"}]}`))
	require.NoError(t, err)
	return New(base.getProj(), newMockReplier(), base.fileMapGetter, &MockScheduler{}, base.listPkgs, base.lookupPkgDoc, resources)
}

func TestServerConfiguredResourcesWithSpx(t *testing.T) {
	for _, tt := range []struct{ name, source, context, present, absent string }{
		{"Configured", `use "In|tro"`, "demo://clips", "Finale", "Beep"},
		{"SDK", `play "In|tro"`, "spx://resources/sounds", "Beep", "Finale"},
		{"ConfiguredConversion", `use Clip(sdk.SoundName("In|tro"))`, "demo://clips", "Finale", "Beep"},
		{"ConfiguredNested", `use Clip(string(sdk.SoundName("In|tro")))`, "demo://clips", "Finale", "Beep"},
		{"SDKNested", `play string(Clip("In|tro"))`, "spx://resources/sounds", "Beep", "Finale"},
		{"EmptyConfiguredConversion", `use Clip(sdk.SoundName("|"))`, "demo://clips", "Finale", "Beep"},
		{"EmptySDKConversion", `play string(Clip("|"))`, "spx://resources/sounds", "Beep", "Finale"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, tt.source+"\n")
			pos.Line += 3
			s := newMixedResourceTestServer(t, source)
			proj := s.requestProject()
			_, err := proj.TypeInfo()
			require.NoError(t, err)
			links, err := documentLinksForResources(proj, "main.spx")
			require.NoError(t, err)
			if strings.Contains(source, `"Intro"`) {
				require.Len(t, links, 1)
				require.NotNil(t, links[0].Target)
				assert.Equal(t, tt.context+"/Intro", string(*links[0].Target))
			}
			labels := completionItemLabels(completionItemsAt(t, s, "main.spx", pos))
			assert.Contains(t, labels, tt.present)
			assert.NotContains(t, labels, tt.absent)
			slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"}}})
			require.NoError(t, err)
			if strings.Contains(source, `"Intro"`) {
				var values []any
				for _, slot := range slots {
					if slot.Input.Type == XGoInputTypeResourceName {
						values = append(values, slot.Input.Value)
					}
				}
				assert.Equal(t, []any{XGoResourceURI(tt.context + "/Intro")}, values)
			}
		})
	}
	t.Run("SharedConstantBatchRename", func(t *testing.T) {
		s := newMixedResourceTestServer(t, "const name = \"Intro\"\nplay name\nuse name\nvar plain = name\n")
		edit, err := s.renameResources([]XGoRenameResourceParams{
			{Resource: XGoResourceIdentifier{URI: "demo://clips/Intro"}, NewName: "Next"},
			{Resource: XGoResourceIdentifier{URI: "spx://resources/sounds/Intro"}, NewName: "Sound"},
		})
		require.NoError(t, err)
		require.NotNil(t, edit)
		file, ok := s.getProj().File("main.spx")
		require.True(t, ok)
		updated := applyResourceRenameTestEdits(t, string(file.Content), edit.Changes["file:///main.spx"])
		assert.Contains(t, updated, "const name = \"Intro\"\nplay \"Sound\"\nuse \"Next\"\nvar plain = name\n")
	})
	t.Run("UnresolvedSDKScope", func(t *testing.T) {
		source, pos := typeDisplayTestSource(t, "func Wear(name sdk.SpriteCostumeName) {}\nwear string(Clip(\"In|tro\"))\n")
		pos.Line += 3
		s := newMixedResourceTestServer(t, source)
		links, err := documentLinksForResources(s.requestProject(), "main.spx")
		require.NoError(t, err)
		assert.Empty(t, links)
		labels := completionItemLabels(completionItemsAt(t, s, "main.spx", pos))
		assert.NotContains(t, labels, "Finale")
		slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"}}})
		require.NoError(t, err)
		for _, slot := range slots {
			assert.NotEqual(t, ToPtr(XGoResourceContextURI("demo://clips")), slot.Accept.ResourceContext)
		}
	})
}

func TestServerConfiguredResourceIncompleteType(t *testing.T) {
	const configuration = `{"dataFile":"resources.json","types":[{"pkgPath":"main","typeName":"Asset","contextURI":"demo://assets"}]}`
	s := newConfiguredResourceTestServer(t, map[string][]byte{
		"main.xgo":       []byte("type Asset string\nvar asset Asset = \"Intro\"\n"),
		"resources.json": []byte(`{"demo://assets":["Intro"]}`),
	}, configuration)
	old := s.requestProject()
	_, err := analyzeFramework(old)
	require.NoError(t, err)
	s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte("type Other string\nvar asset Asset = \"Intro\"\n"), Version: 1}})
	diagnostics, err := s.diagnosticsAt(s.requestProject())
	require.NoError(t, err)
	assert.NotEmpty(t, diagnostics.diagnostics["file:///main.xgo"])
	assert.True(t, slices.ContainsFunc(diagnostics.diagnostics["file:///resources.json"], func(diagnostic Diagnostic) bool {
		return strings.Contains(diagnostic.Message, "resource type main.Asset not found")
	}))
	s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte("type Asset string\nvar asset Asset = \"Intro\"\n"), Version: 2}})
	links, err := documentLinksForResources(s.requestProject(), "main.xgo")
	require.NoError(t, err)
	assert.Len(t, links, 1)
	links, err = documentLinksForResources(old, "main.xgo")
	require.NoError(t, err)
	assert.Len(t, links, 1)
}

func TestServerConfiguredResourceTypeBindings(t *testing.T) {
	for _, tt := range []struct{ name, declaration, binding, parameter string }{
		{"Defined", "type Asset string", "Asset", "Asset"},
		{"Alias", "type Asset string\ntype Alias = Asset", "Alias", "Alias"},
		{"BuiltinAlias", "type Asset = string", "Asset", "Asset"},
		{"AliasChain", "type Asset = string\ntype Alias = Asset", "Asset", "Alias"},
		{"DefinedAliasChain", "type Asset string\ntype Alias = Asset", "Asset", "Alias"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			configuration, err := json.Marshal(map[string]any{"dataFile": "resources.json", "types": []config.ResourceType{{PkgPath: "main", TypeName: tt.binding, ContextURI: "demo://assets"}}})
			require.NoError(t, err)
			source := tt.declaration + "\nfunc Use(name " + tt.parameter + ") {}\nuse \"Intro\"\ntype Other string\nvar other Other = \"Intro\"\n"
			s := newConfiguredResourceTestServer(t, map[string][]byte{"main.xgo": []byte(source), "resources.json": []byte(`{"demo://assets":["Intro"]}`)}, string(configuration))
			proj := s.requestProject()
			_, err = proj.TypeInfo()
			require.NoError(t, err)
			links, err := documentLinksForResources(proj, "main.xgo")
			require.NoError(t, err)
			require.Len(t, links, 1)
			assert.Equal(t, ToPtr(URI("demo://assets/Intro")), links[0].Target)
		})
	}

}

func TestServerConfiguredResourceUnavailableData(t *testing.T) {
	for _, tt := range []struct{ name, manifest string }{
		{"Missing", ""},
		{"Malformed", `{`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string][]byte{"main.xgo": []byte("import clips \"example.com/support/v2\"\nclips.Accept \"Intro\"\n")}
			if tt.manifest != "" {
				files["resources.json"] = []byte(tt.manifest)
			}
			s := newConfiguredResourceTestServer(t, files, resourceTestConfig)
			proj := s.requestProject()
			analysis, err := analyzeFramework(proj)
			require.NoError(t, err)
			require.NotNil(t, analysis)
			assert.Len(t, analysis.resources.resourceRefs, 1)
			require.Len(t, analysis.resources.diagnostics, 1)
			assert.Equal(t, "resources.json", analysis.resources.diagnostics[0].filename)
			links, err := documentLinksForResources(proj, "main.xgo")
			require.NoError(t, err)
			assert.Empty(t, links)
			edit, err := s.renameResources([]XGoRenameResourceParams{{Resource: XGoResourceIdentifier{URI: "demo://resources/clips/Intro"}, NewName: "Next"}})
			assert.ErrorContains(t, err, "failed to load resources")
			assert.Nil(t, edit)
		})
	}
}

func TestServerConfiguredResourceEscaping(t *testing.T) {
	const source = "import clips \"example.com/support/v2\"\nclips.Accept \"A/B ?#\\\"\\U0001f680\"\n"
	const uri = XGoResourceURI("demo://resources/clips/A%2FB%20%3F%23%22%F0%9F%9A%80")
	s := newConfiguredResourceTestServer(t, map[string][]byte{
		"main.xgo":       []byte(source),
		"resources.json": []byte(`{"demo://resources/clips":["A/B ?#\"\ud83d\ude80"]}`),
	}, resourceTestConfig)
	links, err := documentLinksForResources(s.requestProject(), "main.xgo")
	require.NoError(t, err)
	require.Len(t, links, 1)
	assert.Equal(t, ToPtr(URI(uri)), links[0].Target)
	edit, err := s.renameResources([]XGoRenameResourceParams{{Resource: XGoResourceIdentifier{URI: uri}, NewName: "Next/Clip"}})
	require.NoError(t, err)
	require.NotNil(t, edit)
	updated := applyResourceRenameTestEdits(t, source, edit.Changes["file:///main.xgo"])
	assert.Equal(t, "import clips \"example.com/support/v2\"\nclips.Accept \"Next/Clip\"\n", updated)
}

func TestServerConfiguredResourceSpxIsolation(t *testing.T) {
	t.Run("ReservedCollection", func(t *testing.T) {
		base := newMixedResourceTestServer(t, "use \"Intro\"\n")
		resources, err := config.ParseResourceConfig([]byte(`{"dataFile":"resources.json","types":[{"pkgPath":"main","typeName":"Clip","contextURI":"spx://resources/sounds"}]}`))
		require.NoError(t, err)
		s := New(base.getProj(), newMockReplier(), base.fileMapGetter, &MockScheduler{}, base.listPkgs, base.lookupPkgDoc, resources)
		analysis, err := analyzeFramework(s.requestProject())
		require.NoError(t, err)
		require.NotNil(t, analysis)
		assert.True(t, slices.ContainsFunc(analysis.resources.diagnostics, func(diagnostic sourceDiagnostic) bool {
			return strings.Contains(diagnostic.diagnostic.Message, "already handled by a framework adapter")
		}))
		diagnostics, err := s.diagnosticsAt(s.requestProject())
		require.NoError(t, err)
		assert.NotEmpty(t, diagnostics.diagnostics["file:///resources.json"])
		edit, err := s.renameResources([]XGoRenameResourceParams{{Resource: XGoResourceIdentifier{URI: "spx://resources/sounds/Intro"}, NewName: "Next"}})
		require.NoError(t, err)
		require.NotNil(t, edit)

	})
	t.Run("ReservedType", func(t *testing.T) {
		base := newMixedResourceTestServer(t, "use \"Intro\"\nplay \"Intro\"\n")
		resources, err := config.ParseResourceConfig([]byte(`{"dataFile":"resources.json","types":[{"pkgPath":"github.com/goplus/spx/v3","typeName":"SoundName","contextURI":"demo://clips"},{"pkgPath":"main","typeName":"Clip","contextURI":"demo://clips"}]}`))
		require.NoError(t, err)
		s := New(base.getProj(), newMockReplier(), base.fileMapGetter, &MockScheduler{}, base.listPkgs, base.lookupPkgDoc, resources)
		analysis, err := analyzeFramework(s.requestProject())
		require.NoError(t, err)
		require.NotNil(t, analysis)
		require.Len(t, analysis.resources.diagnostics, 1)
		assert.Contains(t, analysis.resources.diagnostics[0].diagnostic.Message, "SoundName is already handled by a framework adapter")
		links, err := documentLinksForResources(s.requestProject(), "main.spx")
		require.NoError(t, err)
		var targets []URI
		for _, link := range links {
			require.NotNil(t, link.Target)
			targets = append(targets, *link.Target)
		}
		assert.ElementsMatch(t, []URI{"demo://clips/Intro", "spx://resources/sounds/Intro"}, targets)
	})

	for _, tt := range []struct{ name, file, uri string }{
		{"InvalidSDKData", "assets/index.json", "demo://clips/Intro"},
		{"InvalidConfiguredData", "resources.json", "spx://resources/sounds/Intro"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newMixedResourceTestServer(t, "use \"Intro\"\nplay \"Intro\"\n")
			s.ModifyFiles([]FileChange{{Path: tt.file, Content: []byte(`{`), Version: 1}})
			links, err := documentLinksForResources(s.requestProject(), "main.spx")
			require.NoError(t, err)
			require.Len(t, links, 1)
			assert.Equal(t, ToPtr(URI(tt.uri)), links[0].Target)
			edit, err := s.renameResources([]XGoRenameResourceParams{{Resource: XGoResourceIdentifier{URI: XGoResourceURI(tt.uri)}, NewName: "Next"}})
			require.NoError(t, err)
			require.NotNil(t, edit)
			assert.Len(t, edit.Changes["file:///main.spx"], 1)
		})
	}
}

func TestServerConfiguredResourceAliasIdentity(t *testing.T) {
	for _, tt := range []struct{ name, base string }{{"DistinctAliases", "string"}, {"ExplicitAliasBinding", "Asset"}} {
		t.Run(tt.name, func(t *testing.T) {
			const configuration = `{"dataFile":"resources.json","types":[{"pkgPath":"main","typeName":"Asset","contextURI":"demo://assets"},{"pkgPath":"main","typeName":"Scene","contextURI":"demo://scenes"}]}`
			source := "type Asset = string\ntype Scene = " + tt.base + `
type Alias = Scene
func UseAsset(name Asset) {}
func UseScene(name Alias) {}
useAsset "Intro"
useScene "Intro"
const asset Asset = "Intro"
const scene Alias = "Intro"
var plain string = "Intro"
`
			s := newConfiguredResourceTestServer(t, map[string][]byte{
				"main.xgo":       []byte(source),
				"resources.json": []byte(`{"demo://assets":["Intro"],"demo://scenes":["Intro"]}`),
			}, configuration)
			proj := s.requestProject()
			_, err := proj.TypeInfo()
			require.NoError(t, err)
			links, err := documentLinksForResources(proj, "main.xgo")
			require.NoError(t, err)
			var targets []URI
			for _, link := range links {
				require.NotNil(t, link.Target)
				targets = append(targets, *link.Target)
			}
			assert.ElementsMatch(t, []URI{"demo://assets/Intro", "demo://scenes/Intro", "demo://assets/Intro", "demo://scenes/Intro"}, targets)
			edit, err := s.renameResources([]XGoRenameResourceParams{{Resource: XGoResourceIdentifier{URI: "demo://scenes/Intro"}, NewName: "Next"}})
			require.NoError(t, err)
			require.NotNil(t, edit)
			updated := applyResourceRenameTestEdits(t, source, edit.Changes["file:///main.xgo"])
			assert.Contains(t, updated, `useAsset "Intro"`)
			assert.Contains(t, updated, `useScene "Next"`)
			assert.Contains(t, updated, `const asset Asset = "Intro"`)
			assert.Contains(t, updated, `const scene Alias = "Next"`)
			assert.Contains(t, updated, `var plain string = "Intro"`)
		})
	}
}

func TestServerConfiguredResourcePartialBindings(t *testing.T) {
	for _, tt := range []struct {
		name, context string
		missingFirst  bool
	}{
		{"SeparateCollections", "demo://missing", true},
		{"SharedCollectionMissingFirst", "demo://assets", true},
		{"SharedCollectionMissingLast", "demo://assets", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			bindings := []config.ResourceType{
				{PkgPath: "main", TypeName: "Missing", ContextURI: tt.context},
				{PkgPath: "main", TypeName: "Asset", ContextURI: "demo://assets"},
			}
			if !tt.missingFirst {
				slices.Reverse(bindings)
			}
			configuration, err := json.Marshal(map[string]any{"dataFile": "resources.json", "types": bindings})
			require.NoError(t, err)
			s := newConfiguredResourceTestServer(t, map[string][]byte{
				"main.xgo":       []byte("type Asset = string\nfunc Use(name Asset) {}\nuse \"Intro\"\n"),
				"resources.json": []byte(`{"demo://assets":["Intro","Next"]}`),
			}, string(configuration))
			proj := s.requestProject()
			links, err := documentLinksForResources(proj, "main.xgo")
			require.NoError(t, err)
			require.Len(t, links, 1)
			assert.Equal(t, ToPtr(URI("demo://assets/Intro")), links[0].Target)
			analysis, err := analyzeFramework(proj)
			require.NoError(t, err)
			require.Len(t, analysis.resources.diagnostics, 1)
			assert.Contains(t, analysis.resources.diagnostics[0].diagnostic.Message, "main.Missing not found")
			labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", Position{Line: 2, Character: 7}))
			assert.Contains(t, labels, "Next")
			edit, err := s.renameResources([]XGoRenameResourceParams{{Resource: XGoResourceIdentifier{URI: "demo://assets/Intro"}, NewName: "Finale"}})
			require.NoError(t, err)
			require.NotNil(t, edit)
			assert.Len(t, edit.Changes["file:///main.xgo"], 1)
		})
	}
}

func TestServerConfiguredResourcePartialCollections(t *testing.T) {
	for _, tt := range []struct{ name, manifest string }{
		{"Null", `{"demo://resources/clips":null,"demo://resources/scenes":["Intro","Studio"]}`},
		{"WrongType", `{"demo://resources/clips":42,"demo://resources/scenes":["Intro","Studio"]}`},
		{"InvalidName", `{"demo://resources/clips":["Intro",""],"demo://resources/scenes":["Intro","Studio"]}`},
		{"UnknownCollection", `{"other://assets":[],"demo://resources/scenes":["Intro","Studio"]}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newConfiguredResourceTestServer(t, map[string][]byte{
				"main.xgo":       []byte("import clips \"example.com/support/v2\"\nimport scenes \"example.com/alternate\"\nclips.Accept \"Intro\"\nscenes.Accept \"Intro\"\n"),
				"resources.json": []byte(tt.manifest),
			}, resourceTestConfig)
			links, err := documentLinksForResources(s.requestProject(), "main.xgo")
			require.NoError(t, err)
			require.Len(t, links, 1)
			assert.Equal(t, ToPtr(URI("demo://resources/scenes/Intro")), links[0].Target)
			labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", Position{Line: 3, Character: 17}))
			assert.Contains(t, labels, "Studio")
			edit, err := s.renameResources([]XGoRenameResourceParams{{Resource: XGoResourceIdentifier{URI: "demo://resources/scenes/Intro"}, NewName: "Next"}})
			require.NoError(t, err)
			require.NotNil(t, edit)
			assert.Len(t, edit.Changes["file:///main.xgo"], 1)
			if tt.name != "UnknownCollection" {
				edit, err = s.renameResources([]XGoRenameResourceParams{{Resource: XGoResourceIdentifier{URI: "demo://resources/clips/Intro"}, NewName: "Next"}})
				assert.ErrorContains(t, err, "invalid resource collection")
				assert.Nil(t, edit)
			}
		})
	}
}

func TestServerConfiguredResourcePlaceholders(t *testing.T) {
	for _, tt := range []struct{ name, source, value string }{
		{"Empty", `""`, ""},
		{"InvalidUTF8", `"\xff"`, "\xff"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, provider := range []struct{ name, call, context string }{
				{"Configured", "use", "demo://clips"},
				{"SDK", "play", "spx://resources/sounds"},
			} {
				t.Run(provider.name, func(t *testing.T) {
					s := newMixedResourceTestServer(t, provider.call+" "+tt.source+"\n")
					slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"}}})
					require.NoError(t, err)
					require.Len(t, slots, 1)
					assert.Equal(t, XGoInputTypeResourceName, slots[0].Accept.Type)
					assert.Equal(t, ToPtr(XGoResourceContextURI(provider.context)), slots[0].Accept.ResourceContext)
					assert.Equal(t, XGoInputTypeString, slots[0].Input.Type)
					assert.Equal(t, tt.value, slots[0].Input.Value)
					links, err := documentLinksForResources(s.requestProject(), "main.spx")
					require.NoError(t, err)
					assert.Empty(t, links)
				})
			}
		})
	}
}

func TestServerConfiguredResourceRenameValidation(t *testing.T) {
	for _, provider := range []struct{ name, context string }{
		{"Configured", "demo://clips"}, {"SDK", "spx://resources/sounds"},
	} {
		t.Run(provider.name, func(t *testing.T) {
			for _, tt := range []struct {
				name           string
				names, sources []string
				wantError      string
			}{
				{"Empty", []string{""}, []string{"Intro"}, "invalid resource name"},
				{"InvalidUTF8", []string{"\xff"}, []string{"Intro"}, "invalid resource name"},
				{"Query", []string{"Next"}, []string{"Intro?ignored"}, "invalid"},
				{"EmptyQuery", []string{"Next"}, []string{"Intro?"}, "invalid"},
				{"Fragment", []string{"Next"}, []string{"Intro#ignored"}, "invalid"},
				{"InvalidSourceUTF8", []string{"Next"}, []string{"%FF"}, "invalid"},
				{"ConflictingSource", []string{"Intro", "Next"}, []string{"Intro", "%49ntro"}, "conflicting renames"},
				{"ConflictingTarget", []string{"Next", "Next"}, []string{"Intro", "Other"}, "conflicting rename target"},
				{"DuplicateSource", []string{"Next", "Next"}, []string{"Intro", "%49ntro"}, ""},
				{"NoOp", []string{"Intro"}, []string{"Intro"}, ""},
				{"DotName", []string{"."}, []string{"Intro"}, ""},
			} {
				t.Run(tt.name, func(t *testing.T) {
					s := newMixedResourceTestServer(t, "use \"Intro\"\nplay \"Intro\"\n")
					var params []XGoRenameResourceParams
					for i, source := range tt.sources {
						params = append(params, XGoRenameResourceParams{Resource: XGoResourceIdentifier{URI: XGoResourceURI(provider.context + "/" + source)}, NewName: tt.names[i]})
					}
					edit, err := s.renameResources(params)
					if tt.wantError != "" {
						assert.ErrorContains(t, err, tt.wantError)
						assert.Nil(t, edit)
						return
					}
					require.NoError(t, err)
					require.NotNil(t, edit)
					if tt.name == "NoOp" {
						assert.Empty(t, edit.Changes)
					} else {
						assert.Len(t, edit.Changes["file:///main.spx"], 1)
					}
				})
			}
		})
	}
}

func TestResolveResourceType(t *testing.T) {
	t.Run("ErasedAlias", func(t *testing.T) {
		s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte("var plain = \"Intro\"\n")})
		proj := s.getProj()
		pkg := gotypes.NewPackage("example.com/assets", "assets")
		pkg.Scope().Insert(gotypes.NewTypeName(0, pkg, "Asset", gotypes.Typ[gotypes.String]))
		pkg.MarkComplete()
		base := proj.Importer
		proj.Importer = testImporterFunc(func(path string) (*gotypes.Package, error) {
			if path == pkg.Path() {
				return pkg, nil
			}
			return base.Import(path)
		})
		typ, err := resolveResourceType(proj, config.ResourceType{PkgPath: pkg.Path(), TypeName: "Asset"})
		assert.ErrorContains(t, err, "must retain its declaration identity")
		assert.Nil(t, typ)
	})
}
