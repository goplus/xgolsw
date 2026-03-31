package spx

import (
	"testing"

	"github.com/goplus/xgo/scanner"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgo/x/typesutil"
	"github.com/goplus/xgolsw/internal/classfile"
	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderBuild(t *testing.T) {
	t.Run("ValidProject", func(t *testing.T) {
		proj := newTestProject(defaultProjectFiles(), xgo.FeatASTCache)
		snapshot, err := (provider{}).Build(&classfile.BuildContext{Project: proj})
		require.NoError(t, err)
		require.NotNil(t, snapshot)
		assert.Equal(t, ProviderID, snapshot.Provider)

		value, ok := snapshot.Resources.Get(ResourceSetKey)
		require.True(t, ok)
		set, ok := value.(*ResourceSet)
		require.True(t, ok)
		assert.NotNil(t, set.Sprite("Hero"))

		mainFile, ok := snapshot.Resources.Get(MainFileKey)
		assert.True(t, ok)
		assert.Equal(t, "main.spx", mainFile)
	})

	t.Run("NoSpxFiles", func(t *testing.T) {
		proj := newTestProject(map[string]string{
			"assets/index.json": `{}`,
		}, xgo.FeatASTCache)
		snapshot, err := (provider{}).Build(&classfile.BuildContext{Project: proj})
		require.NoError(t, err)
		msgs := diagnosticMessages(snapshot.Diagnostics)
		assert.Contains(t, msgs, "no spx files found")
		assert.Contains(t, msgs, "no valid main.spx file found in main package")
		_, ok := snapshot.Resources.Get(ResourceSetKey)
		assert.False(t, ok)
	})

	t.Run("InvalidMainPackage", func(t *testing.T) {
		files := defaultProjectFiles()
		files["main.spx"] = `package hero

Hero.
run "assets", {Title: "My Game"}
`
		proj := newTestProject(files, xgo.FeatASTCache)
		snapshot, err := (provider{}).Build(&classfile.BuildContext{Project: proj})
		require.NoError(t, err)
		msgs := diagnosticMessages(snapshot.Diagnostics)
		assert.Contains(t, msgs, "package name must be main")
	})
}

func TestCollectSpxFiles(t *testing.T) {
	files := defaultProjectFiles()
	files["scripts/helper.gop"] = "package helper"
	files["README.md"] = "irrelevant"
	proj := newTestProject(files, 0)

	list := collectSpxFiles(proj)
	assert.ElementsMatch(t, []string{"Hero.spx", "main.spx"}, list)
}

func TestAnalyzeFile(t *testing.T) {
	t.Run("MainPackage", func(t *testing.T) {
		proj := newTestProject(defaultProjectFiles(), xgo.FeatASTCache)

		isMain, _ := analyzeFile(proj, "main.spx")
		assert.True(t, isMain)
	})

	t.Run("NonMainPackage", func(t *testing.T) {
		files := defaultProjectFiles()
		files["main.spx"] = `package hero

Hero.
run "assets", {Title: "My Game"}
`
		proj := newTestProject(files, xgo.FeatASTCache)

		isMain, diags := analyzeFile(proj, "main.spx")
		assert.False(t, isMain)
		require.NotEmpty(t, diags)
		found := false
		for _, diag := range diags {
			if diag.Msg == "package name must be main" {
				found = true
				break
			}
		}
		assert.True(t, found, "expected package name diagnostic, got %v", diags)
	})
}

func TestScannerErrorRange(t *testing.T) {
	fset := token.NewFileSet()
	file := fset.AddFile("main.spx", -1, len("package main"))
	file.SetLinesForContent([]byte("package main"))

	err := &scanner.Error{Pos: token.Position{Filename: "main.spx", Line: 1, Column: 8, Offset: 7}}
	start, end := scannerErrorRange(fset, err)
	assert.True(t, start.IsValid())
	assert.Equal(t, start, end)

	noFileErr := &scanner.Error{Pos: token.Position{Filename: "missing.spx", Line: 1, Column: 1}}
	start, end = scannerErrorRange(fset, noFileErr)
	assert.False(t, start.IsValid())
	assert.False(t, end.IsValid())
}

func diagnosticMessages(diags []typesutil.Error) []string {
	msgs := make([]string, len(diags))
	for i, diag := range diags {
		msgs[i] = diag.Msg
	}
	return msgs
}
