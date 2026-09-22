package server

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/goplus/xgolsw/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type propertyNotificationReplierFunc func(jsonrpc2.Message) error

func (f propertyNotificationReplierFunc) ReplyMessage(message jsonrpc2.Message) error {
	return f(message)
}

func TestServerTextDocumentRenamePropertyNotifications(t *testing.T) {
	for _, tt := range []struct{ name, declaration, use, oldName, newName string }{
		{"MethodDeclaration", "func (Base) La|bel() string { return \"\" }", "base.label", "label", "Title"},
		{"PropertyRead", "func (Base) Label() string { return \"\" }", "base.la|bel", "label", "title"},
		{"Field", "", "base.Co|unt", "Count", "Total"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, "type Base struct { Count int }\n"+tt.declaration+`
type Derived struct { Base }
type Shadow struct { Base; Count []int; label string }
type Alias = Derived
type Pointer = *Alias
type Indirect = **Alias
var base Base
_ = `+tt.use+"\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			replier := newMockReplier()
			s.replier = replier
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			edit, err := s.textDocumentRename(&RenameParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: pos, NewName: tt.newName})
			require.NoError(t, err)
			require.NotNil(t, edit)
			messages := replier.getMessages()
			require.Len(t, messages, 4)
			var targets []string
			for _, message := range messages {
				notification := requireValueAs[*jsonrpc2.Notification](t, message)
				var params PropertyRenamedParams
				require.NoError(t, json.Unmarshal(notification.Params(), &params))
				assert.Equal(t, "textDocument/xgo.propertyRenamed", notification.Method())
				assert.Equal(t, tt.oldName, params.OldName)
				wantName := "title"
				if tt.name == "Field" {
					wantName = "Total"
				}
				assert.Equal(t, wantName, params.NewName)
				targets = append(targets, params.Target)
				properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: params.Target})
				require.NoError(t, err)
				assert.True(t, slices.ContainsFunc(properties, func(p XGoProperty) bool { return p.Name == params.OldName }))
			}
			assert.ElementsMatch(t, []string{"Base", "Derived", "Alias", "Pointer"}, targets)
			updated := applyResourceRenameTestEdits(t, source, edit.Changes["file:///main.xgo"])
			s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(updated), Version: 1}})
			_, err = s.requestProject().TypeInfo()
			require.NoError(t, err)
			for _, target := range targets {
				properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: target})
				require.NoError(t, err)
				wantName := functionAliasName(tt.newName)
				if tt.name == "Field" {
					wantName = tt.newName
				}
				assert.True(t, slices.ContainsFunc(properties, func(p XGoProperty) bool { return p.Name == wantName }))
				assert.False(t, slices.ContainsFunc(properties, func(p XGoProperty) bool { return p.Name == tt.oldName }))
			}
		})
	}
}

func TestServerTextDocumentRenamePropertyNotificationFailures(t *testing.T) {
	for _, tt := range []struct{ name, source, oldName, newName string }{
		{"Field", `type Base struct { Co|unt int }
type Derived struct { Base }
var base Base
_ = base.Count
`, "Count", "Total"},
		{"Method", `type Reader interface { Label() string }
type Base struct{}
func (Base) La|bel() string { return "" }
type Derived struct { Base }
var reader Reader = Base{}
_ = reader.Label()
_ = Base{}.label
`, "Label", "Title"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, failure := range []struct {
				name    string
				attempt int
			}{
				{"FirstNotification", 1},
				{"AfterOneNotification", 2},
			} {
				t.Run(failure.name, func(t *testing.T) {
					source, pos := typeDisplayTestSource(t, tt.source)
					s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
					proj := s.requestProject()
					_, err := proj.TypeInfo()
					require.NoError(t, err)
					var notifications []PropertyRenamedParams
					s.replier = propertyNotificationReplierFunc(func(message jsonrpc2.Message) error {
						notification := requireValueAs[*jsonrpc2.Notification](t, message)
						require.Equal(t, "textDocument/xgo.propertyRenamed", notification.Method())
						var params PropertyRenamedParams
						require.NoError(t, json.Unmarshal(notification.Params(), &params))
						notifications = append(notifications, params)
						if len(notifications) == failure.attempt {
							return errors.New("notification delivery failed")
						}
						return nil
					})
					edit, err := s.textDocumentRename(&RenameParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: pos, NewName: tt.newName})
					require.NoError(t, err)
					require.NotNil(t, edit)
					assert.Len(t, notifications, failure.attempt)
					require.NotEmpty(t, edit.Changes["file:///main.xgo"])
					updated := applyResourceRenameTestEdits(t, source, edit.Changes["file:///main.xgo"])
					want := strings.NewReplacer(tt.oldName, tt.newName, functionAliasName(tt.oldName), functionAliasName(tt.newName)).Replace(source)
					assert.Equal(t, want, updated)
					file, ok := proj.File("main.xgo")
					require.True(t, ok)
					assert.Equal(t, source, string(file.Content))
					preview := proj.Fork()
					updatedFile := *file
					updatedFile.Content = []byte(updated)
					preview.PutFile("main.xgo", &updatedFile)
					_, err = preview.TypeInfo()
					require.NoError(t, err)
				})
			}
		})
	}
}

func TestServerTextDocumentRenameRemovedProperties(t *testing.T) {
	for _, tt := range []struct{ name, newName string }{
		{"Lowercase", "title"},
		{"Unicode", "\u0393amma"},
		{"Underscore", "_title"},
		{"Internal", "XGo_title"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, "type Record struct{}\nfunc (Record) La|bel() string { return \"\" }\nvar record Record\n_ = record.label\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			replier := newMockReplier()
			s.replier = replier
			edit, err := s.textDocumentRename(&RenameParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: pos, NewName: tt.newName})
			require.NoError(t, err)
			require.NotNil(t, edit)
			assert.Empty(t, replier.getMessages(), "removing a property does not rename it to another property")
			updated := applyResourceRenameTestEdits(t, source, edit.Changes["file:///main.xgo"])
			s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(updated), Version: 1}})
			_, err = s.requestProject().TypeInfo()
			require.NoError(t, err)
			properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: "Record"})
			require.NoError(t, err)
			assert.Empty(t, properties)
		})
	}
}

func TestServerTextDocumentRenamePropertyVisibility(t *testing.T) {
	for _, tt := range []struct {
		name, source, newName string
		want                  []string
	}{
		{name: "AliasField", source: "type Record struct { title string }\nfunc (Record) La|bel() string { return \"\" }\n", newName: "Title"},
		{name: "AliasSliceField", source: "type Record struct { title []string }\nfunc (Record) La|bel() string { return \"\" }\n", newName: "Title"},
		{name: "PromotedGetter", source: "type Base struct{}\nfunc (Base) La|bel() string { return \"\" }\ntype Record struct { Base; title string }\n", newName: "Title", want: []string{"Base"}},
		{name: "PromotedField", source: "type Base struct { title string }\ntype Record struct { Base }\nfunc (Record) La|bel() string { return \"\" }\n", newName: "Title", want: []string{"Record"}},
		{name: "InternalField", source: "type Record struct { Va|lue string }\n", newName: "XGo_value"},
		{name: "NoChange", source: "type Record struct { Va|lue string }\n", newName: "Value"},
		{name: "UnicodeField", source: "type Record struct { /* \U0001f600 */ Va|lue string }\n", newName: "\u0393amma", want: []string{"Record"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, tt.source)
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			replier := newMockReplier()
			s.replier = replier
			proj := s.requestProject()
			info, err := proj.TypeInfo()
			require.NoError(t, err)
			fileSetBase := proj.Fset.Base()
			edit, err := s.textDocumentRename(&RenameParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: pos, NewName: tt.newName})
			require.NoError(t, err)
			require.NotNil(t, edit)
			file, ok := proj.File("main.xgo")
			require.True(t, ok)
			assert.Equal(t, source, string(file.Content))
			currentInfo, err := proj.TypeInfo()
			require.NoError(t, err)
			assert.Same(t, info, currentInfo)
			assert.Equal(t, fileSetBase, proj.Fset.Base())
			var targets []string
			var notifications []PropertyRenamedParams
			for _, message := range replier.getMessages() {
				notification := requireValueAs[*jsonrpc2.Notification](t, message)
				var params PropertyRenamedParams
				require.NoError(t, json.Unmarshal(notification.Params(), &params))
				targets = append(targets, params.Target)
				notifications = append(notifications, params)
			}
			assert.ElementsMatch(t, tt.want, targets)
			updated := applyResourceRenameTestEdits(t, source, edit.Changes["file:///main.xgo"])
			s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(updated), Version: 1}})
			_, err = s.requestProject().TypeInfo()
			require.NoError(t, err)
			for _, params := range notifications {
				properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: params.Target})
				require.NoError(t, err)
				assert.True(t, slices.ContainsFunc(properties, func(p XGoProperty) bool { return p.Name == params.NewName }))
			}
		})
	}
}

func TestServerTextDocumentRenamePropertyDeclarationOffsets(t *testing.T) {
	for _, lineEnding := range []struct{ name, value string }{{"LF", "\n"}, {"CRLF", "\r\n"}} {
		t.Run(lineEnding.name, func(t *testing.T) {
			declarations := "/* \U0001f600 */ " + `type Reader interface { Label() string }
type Record struct{}
func (Record) La|bel() string { return "" }; func (Other) Label() string { return "" }
type Other struct{}
type Derived struct { Record }
type Alias = Derived
`
			source, pos := typeDisplayTestSource(t, strings.ReplaceAll(declarations, "\n", lineEnding.value))
			s := newImportTestServer(t, map[string][]byte{
				"types.xgo": []byte(source),
				"main.xgo":  []byte("var reader Reader = Record{}\nprintln reader.Label(), Record{}.label, Other{}.label\n"),
			})
			replier := newMockReplier()
			s.replier = replier
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			edit, err := s.textDocumentRename(&RenameParams{TextDocument: TextDocumentIdentifier{URI: "file:///types.xgo"}, Position: pos, NewName: "LongerTitle"})
			require.NoError(t, err)
			require.NotNil(t, edit)
			var targets []string
			for _, message := range replier.getMessages() {
				var params PropertyRenamedParams
				require.NoError(t, json.Unmarshal(requireValueAs[*jsonrpc2.Notification](t, message).Params(), &params))
				assert.Equal(t, "longerTitle", params.NewName)
				targets = append(targets, params.Target)
			}
			assert.ElementsMatch(t, []string{"Record", "Other", "Derived", "Alias"}, targets)
			for filename, file := range s.requestProject().Files() {
				if edits := edit.Changes[s.toDocumentURI(filename)]; len(edits) > 0 {
					s.ModifyFiles([]FileChange{{Path: filename, Content: []byte(applyResourceRenameTestEdits(t, string(file.Content), edits)), Version: 1}})
				}
			}
			_, err = s.requestProject().TypeInfo()
			require.NoError(t, err)
		})
	}
}

func TestServerTextDocumentRenamePropertyIncompleteProject(t *testing.T) {
	source, pos := typeDisplayTestSource(t, "type Record struct { Va|lue int }\n")
	s := newImportTestServer(t, map[string][]byte{
		"main.xgo":   []byte(source),
		"broken.xgo": []byte("func broken() { missing() }\n"),
	})
	replier := newMockReplier()
	s.replier = replier
	_, err := s.requestProject().TypeInfo()
	require.Error(t, err)
	edit, err := s.textDocumentRename(&RenameParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: pos, NewName: "Count"})
	require.NoError(t, err)
	require.NotNil(t, edit)
	require.Len(t, replier.getMessages(), 1)
	var params PropertyRenamedParams
	require.NoError(t, json.Unmarshal(requireValueAs[*jsonrpc2.Notification](t, replier.getMessages()[0]).Params(), &params))
	assert.Equal(t, PropertyRenamedParams{Target: "Record", OldName: "Value", NewName: "Count", TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}, params)
}
