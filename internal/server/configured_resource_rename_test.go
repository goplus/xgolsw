package server

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newResourceAliasTestServer(t *testing.T, source, scene, clip string) *Server {
	t.Helper()
	manifest, err := json.Marshal(map[string][]string{"demo://scenes": {scene}, "demo://clips": {clip}})
	require.NoError(t, err)
	return newConfiguredResourceTestServer(t, map[string][]byte{
		"main_fixture.gox": []byte(source),
		"types.xgo":        []byte("type Scene = string\ntype Clip = string\nfunc scene(value Scene) {}\nfunc clip(value Clip) {}\n"),
		"resources.json":   manifest,
	}, `{"dataFile":"resources.json","types":[{"pkgPath":"main","typeName":"Scene","contextURI":"demo://scenes"},{"pkgPath":"main","typeName":"Clip","contextURI":"demo://clips"}]}`)
}

func TestServerConfiguredResourceAliasRename(t *testing.T) {
	t.Run("LiteralConversions", func(t *testing.T) {
		for _, tt := range []struct {
			name       string
			source     string
			collection string
		}{
			{"Intrinsic", "echo Scene(\"Shared\")\n", "scenes"},
			{"Nested", "echo Scene(string((\"Shared\")))\n", "scenes"},
			{"Initializer", "const name = Scene(\"Shared\")\nscene name\n", "scenes"},
			{"Contextual", "clip Scene(\"Shared\")\n", "clips"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				source := tt.source
				name := "Shared"
				for _, newName := range []string{"Renamed", "Final"} {
					s := newResourceAliasTestServer(t, source, name, name)
					requireNoDiagnostics(t, s)
					uri := XGoResourceURI("demo://" + tt.collection + "/" + name)
					links, err := documentLinksForResources(s.getProj(), "main_fixture.gox")
					require.NoError(t, err)
					require.NotEmpty(t, links)
					for _, link := range links {
						require.NotNil(t, link.Target)
						assert.Equal(t, URI(uri), *link.Target)
					}
					edit, err := s.renameResources([]XGoRenameResourceParams{{Resource: XGoResourceIdentifier{URI: uri}, NewName: newName}})
					require.NoError(t, err)
					require.NotNil(t, edit)
					updated := applyResourceRenameTestEdits(t, source, edit.Changes["file:///main_fixture.gox"])
					assert.Equal(t, strings.ReplaceAll(source, name, newName), updated)
					source = updated
					name = newName
				}
			})
		}
	})

	t.Run("ConstantExpressionDependency", func(t *testing.T) {
		const source = "const base Scene = \"Shared\"\nconst clipName = base + \"Suffix\"\nscene base\nclip clipName\n"
		s := newResourceAliasTestServer(t, source, "Shared", "SharedSuffix")
		requireNoDiagnostics(t, s)
		edit, err := s.renameResources([]XGoRenameResourceParams{{
			Resource: XGoResourceIdentifier{URI: "demo://scenes/Shared"}, NewName: "Scene",
		}})
		require.NoError(t, err)
		require.NotNil(t, edit)
		updated := applyResourceRenameTestEdits(t, source, edit.Changes["file:///main_fixture.gox"])
		assert.Equal(t, "const base Scene = \"Scene\"\nconst clipName = \"SharedSuffix\"\nscene base\nclip clipName\n", updated)
		updatedServer := newResourceAliasTestServer(t, updated, "Scene", "SharedSuffix")
		requireNoDiagnostics(t, updatedServer)
	})

	t.Run("SharedTypedConstants", func(t *testing.T) {
		for _, tt := range []struct {
			name        string
			declaration string
			sceneName   string
			clipName    string
			want        string
		}{
			{"PrimaryResource", "const name Scene = \"Shared\"\n", "Scene", "Shared", "const name Scene = \"Scene\"\nscene name\nclip \"Shared\"\n"},
			{"OtherResource", "const name Scene = \"Shared\"\n", "Shared", "Sound", "const name Scene = \"Shared\"\nscene name\nclip \"Sound\"\n"},
			{"DifferentValues", "const name Scene = \"Shared\"\n", "Scene", "Sound", "const name Scene = \"Scene\"\nscene name\nclip \"Sound\"\n"},
			{"SameValue", "const name Scene = \"Shared\"\n", "Scene", "Scene", "const name Scene = \"Scene\"\nscene name\nclip name\n"},
			{"ConstantDependency", "const base Scene = \"Shared\"\nconst name = base\n", "Scene", "Sound", "const base Scene = \"Scene\"\nconst name = base\nscene name\nclip \"Sound\"\n"},
			{"RepeatedInitializer", "const (\nbase Scene = \"Shared\"\nname\n)\n", "Scene", "Sound", "const (\nbase Scene = \"Scene\"\nname\n)\nscene name\nclip \"Sound\"\n"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				source := tt.declaration + "scene name\nclip name\n"
				s := newResourceAliasTestServer(t, source, "Shared", "Shared")
				requireNoDiagnostics(t, s)
				var params []XGoRenameResourceParams
				if tt.sceneName != "Shared" {
					params = append(params, XGoRenameResourceParams{Resource: XGoResourceIdentifier{URI: "demo://scenes/Shared"}, NewName: tt.sceneName})
				}
				if tt.clipName != "Shared" {
					params = append(params, XGoRenameResourceParams{Resource: XGoResourceIdentifier{URI: "demo://clips/Shared"}, NewName: tt.clipName})
				}
				for range 2 {
					edit, err := s.renameResources(params)
					require.NoError(t, err)
					require.NotNil(t, edit)
					updated := applyResourceRenameTestEdits(t, source, edit.Changes["file:///main_fixture.gox"])
					assert.Equal(t, tt.want, updated)
					updatedServer := newResourceAliasTestServer(t, updated, tt.sceneName, tt.clipName)
					requireNoDiagnostics(t, updatedServer)
					slices.Reverse(params)
				}
			})
		}
	})

	t.Run("SharedConstants", func(t *testing.T) {
		for _, tt := range []struct {
			name      string
			literal   string
			sceneName string
			clipName  string
		}{
			{"SameReplacement", `"Shared"`, "Renamed", "Renamed"},
			{"DifferentReplacements", `"Shared"`, "Scene", "Sound"},
			{"RawString", "`Shared`", "Scene", "Sound`"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				source := "const name = " + tt.literal + "\nscene name\nclip name\n"
				s := newResourceAliasTestServer(t, source, "Shared", "Shared")
				requireNoDiagnostics(t, s)
				for _, batch := range []bool{false, true} {
					params := []XGoRenameResourceParams{{
						Resource: XGoResourceIdentifier{URI: "demo://scenes/Shared"}, NewName: tt.sceneName,
					}}
					want := "const name = " + tt.literal + "\nscene \"" + tt.sceneName + "\"\nclip name\n"
					clipName := "Shared"
					if batch {
						params = append(params, XGoRenameResourceParams{
							Resource: XGoResourceIdentifier{URI: "demo://clips/Shared"}, NewName: tt.clipName,
						})
						want = strings.Replace(want, "clip name", "clip \""+tt.clipName+"\"", 1)
						clipName = tt.clipName
						if tt.sceneName == tt.clipName {
							want = strings.Replace(source, "Shared", tt.sceneName, 1)
						}
					}
					edit, err := s.renameResources(params)
					require.NoError(t, err)
					require.NotNil(t, edit)
					require.Len(t, edit.Changes, 1)
					updated := applyResourceRenameTestEdits(t, source, edit.Changes["file:///main_fixture.gox"])
					assert.Equal(t, want, updated)
					updatedServer := newResourceAliasTestServer(t, updated, tt.sceneName, clipName)
					requireNoDiagnostics(t, updatedServer)
				}
			})
		}
	})

}
