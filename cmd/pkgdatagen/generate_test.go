package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	gotypes "go/types"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/goplus/xgolsw/xgo/xgoutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/gcexportdata"
)

func TestGenerate(t *testing.T) {
	for _, tt := range []struct {
		name    string
		pkgName string
	}{
		{name: "VersionedPackage", pkgName: "fixture"},
		{name: "DifferentPackageName", pkgName: "sample"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const pkgPath = "example.com/fixture/v2"
			t.Chdir(t.TempDir())
			require.NoError(t, os.WriteFile("go.mod", []byte("module "+pkgPath+"\n\ngo 1.25.0\n"), 0o644))
			for name, source := range map[string]string{
				"fixture.go": "// Package " + tt.pkgName + " provides fixture values.\npackage " + tt.pkgName + `

// Limit bounds the number of records.
const Limit = 7

// Current holds the active record.
var Current Record

// Record holds a number.
type Record struct {
    // Number is the stored number.
    Number int
}

// Double returns twice the stored number.
func (r Record) Double() int { return r.Number * 2 }

// Add returns the sum of its arguments.
func Add(left, right int) int { return left + right }
`,
				"platform_js.go":    "package " + tt.pkgName + "\n\n// WasmOnly belongs to the browser build.\nconst WasmOnly = true\n",
				"platform_linux.go": "package " + tt.pkgName + "\n\n// NativeOnly belongs to the native build.\nconst NativeOnly = true\n",
				"fixture_test.go":   "package " + tt.pkgName + "\n\n// TestOnly belongs to the tests.\nconst TestOnly = true\n",
			} {
				require.NoError(t, os.WriteFile(name, []byte(source), 0o644))
			}
			outputFile := "pkgdata.zip"
			require.NoError(t, generate([]string{pkgPath}, outputFile))
			zr, err := zip.OpenReader(outputFile)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, zr.Close()) })
			require.Len(t, zr.File, 2)

			data := readGeneratedFile(t, zr, pkgPath+".pkgexport")
			pkg, err := gcexportdata.Read(bytes.NewReader(data), token.NewFileSet(), make(map[string]*gotypes.Package), pkgPath)
			require.NoError(t, err)
			assert.True(t, pkg.Complete())
			assert.Equal(t, tt.pkgName, pkg.Name())
			assert.Equal(t, pkgPath, pkg.Path())
			assert.Equal(t, []string{"Add", "Current", "Limit", "Record", "WasmOnly"}, pkg.Scope().Names())
			limit, ok := pkg.Scope().Lookup("Limit").(*gotypes.Const)
			require.True(t, ok)
			assert.Equal(t, "7", limit.Val().ExactString())
			add, ok := pkg.Scope().Lookup("Add").(*gotypes.Func)
			require.True(t, ok)
			require.Equal(t, 2, add.Signature().Params().Len())
			assert.Equal(t, "left", add.Signature().Params().At(0).Name())
			assert.Equal(t, gotypes.Typ[gotypes.Int], add.Signature().Params().At(0).Type())

			var doc pkgdoc.PkgDoc
			require.NoError(t, json.Unmarshal(readGeneratedFile(t, zr, pkgPath+".pkgdoc"), &doc))
			assert.Equal(t, pkgdoc.PkgDoc{
				Path: pkgPath, Name: tt.pkgName, Doc: "Package " + tt.pkgName + " provides fixture values.\n",
				Vars: map[string]string{"Current": "Current holds the active record.\n"},
				Consts: map[string]string{
					"Limit":    "Limit bounds the number of records.\n",
					"WasmOnly": "WasmOnly belongs to the browser build.\n",
				},
				Funcs: map[string]string{"Add": "Add returns the sum of its arguments.\n"},
				Types: map[string]*pkgdoc.TypeDoc{"Record": {
					Doc:     "Record holds a number.\n",
					Fields:  map[string]string{"Number": "Number is the stored number.\n"},
					Methods: map[string]string{"Double": "Double returns twice the stored number.\n"},
				}},
			}, doc)
		})
	}

	t.Run("ClassfileFramework", func(t *testing.T) {
		source, err := os.ReadFile(filepath.Join("..", "..", "internal", "testframework", "testdata", "framework.go"))
		require.NoError(t, err)
		t.Chdir(t.TempDir())
		require.NoError(t, os.WriteFile("go.mod", []byte("module "+testframework.PkgPath+"\n\ngo 1.25.0\n"), 0o644))
		require.NoError(t, os.WriteFile("framework.go", source, 0o644))
		require.NoError(t, generate([]string{testframework.PkgPath}, "pkgdata.zip"))
		zr, err := zip.OpenReader("pkgdata.zip")
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, zr.Close()) })
		require.Len(t, zr.File, 2)

		data := readGeneratedFile(t, zr, testframework.PkgPath+".pkgexport")
		pkg, err := gcexportdata.Read(bytes.NewReader(data), token.NewFileSet(), make(map[string]*gotypes.Package), testframework.PkgPath)
		require.NoError(t, err)
		assert.True(t, pkg.Complete())
		assert.Equal(t, testframework.PkgPath, pkg.Path())
		assert.Equal(t, "framework", pkg.Name())
		assert.True(t, xgoutil.IsMarkedAsXGoPackage(pkg))

		var doc pkgdoc.PkgDoc
		require.NoError(t, json.Unmarshal(readGeneratedFile(t, zr, testframework.PkgPath+".pkgdoc"), &doc))
		assert.Equal(t, testframework.PkgPath, doc.Path)
		assert.Equal(t, "framework", doc.Name)
		appDoc, itemDoc := doc.Types["App"], doc.Types["Item"]
		require.NotNil(t, appDoc)
		require.NotNil(t, itemDoc)
		assert.Equal(t, "App is the project base class.\n", appDoc.Doc)
		assert.Equal(t, "Item is the work base class.\n", itemDoc.Doc)
		assert.Equal(t, "Value stores the work item's value.\n", itemDoc.Fields["Value"])

		app := generatedNamedType(t, pkg, "App")
		item := generatedNamedType(t, pkg, "Item")
		itemStruct, ok := item.Underlying().(*gotypes.Struct)
		require.True(t, ok)
		require.Equal(t, 1, itemStruct.NumFields())
		assert.Equal(t, "Value", itemStruct.Field(0).Name())
		assert.Equal(t, gotypes.Typ[gotypes.Int], itemStruct.Field(0).Type())

		for _, tt := range []struct {
			name string
			arg  gotypes.Type
			doc  string
		}{
			{name: "Measure__0", arg: gotypes.Typ[gotypes.Int], doc: "Measure__0 is the integer overload of Measure.\n"},
			{name: "Measure__1", arg: gotypes.Typ[gotypes.String], doc: "Measure__1 is the string overload of Measure.\n"},
		} {
			obj, _, _ := gotypes.LookupFieldOrMethod(gotypes.NewPointer(app), false, pkg, tt.name)
			method, ok := obj.(*gotypes.Func)
			require.True(t, ok, tt.name)
			sig := method.Signature()
			require.NotNil(t, sig.Recv())
			assert.True(t, gotypes.Identical(gotypes.NewPointer(app), sig.Recv().Type()))
			require.Equal(t, 1, sig.Params().Len())
			assert.Equal(t, "value", sig.Params().At(0).Name())
			assert.Equal(t, tt.arg, sig.Params().At(0).Type())
			require.Equal(t, 1, sig.Results().Len())
			assert.Equal(t, gotypes.Typ[gotypes.Int], sig.Results().At(0).Type())
			assert.Equal(t, tt.doc, appDoc.Methods[tt.name])
		}

		create, ok := pkg.Scope().Lookup("XGot_App_XGox_Create").(*gotypes.Func)
		require.True(t, ok)
		createSig := create.Signature()
		require.Equal(t, 1, createSig.TypeParams().Len())
		require.Equal(t, 2, createSig.Params().Len())
		assert.True(t, gotypes.Identical(gotypes.NewPointer(app), createSig.Params().At(0).Type()))
		assert.Equal(t, "name", createSig.Params().At(1).Name())
		assert.Equal(t, gotypes.Typ[gotypes.String], createSig.Params().At(1).Type())
		require.Equal(t, 1, createSig.Results().Len())
		result, ok := createSig.Results().At(0).Type().(*gotypes.Pointer)
		require.True(t, ok)
		assert.Same(t, createSig.TypeParams().At(0), result.Elem())
		assert.True(t, gotypes.Identical(gotypes.Universe.Lookup("any").Type(), createSig.TypeParams().At(0).Constraint()))
		assert.Equal(t, "XGot_App_XGox_Create provides a method with an explicit type argument.\n", appDoc.Methods["Create"])
		assert.NotContains(t, appDoc.Methods, "XGox_Create")

		entry, ok := pkg.Scope().Lookup("XGot_App_Main").(*gotypes.Func)
		require.True(t, ok)
		entrySig := entry.Signature()
		require.Equal(t, 2, entrySig.Params().Len())
		assert.True(t, entrySig.Variadic())
		appConstraint, ok := entrySig.Params().At(0).Type().(*gotypes.Interface)
		require.True(t, ok)
		require.Equal(t, 1, appConstraint.NumMethods())
		assert.Equal(t, "initApp", appConstraint.Method(0).Name())
		assert.Same(t, pkg, appConstraint.Method(0).Pkg())
		assert.True(t, gotypes.Implements(gotypes.NewPointer(app), appConstraint))
		items, ok := entrySig.Params().At(1).Type().(*gotypes.Slice)
		require.True(t, ok)
		itemConstraint, ok := items.Elem().(*gotypes.Interface)
		require.True(t, ok)
		require.Equal(t, 1, itemConstraint.NumMethods())
		assert.Equal(t, "Main", itemConstraint.Method(0).Name())
		assert.True(t, gotypes.Implements(gotypes.NewPointer(item), itemConstraint))
		assert.Equal(t, "XGot_App_Main receives the generated project and work classes.\n", appDoc.Methods["Main"])
		assert.Equal(t, "Main is the work entry point.\n", itemDoc.Methods["Main"])
	})

	t.Run("Builtin", func(t *testing.T) {
		outputFile := filepath.Join(t.TempDir(), "pkgdata.zip")
		require.NoError(t, generate([]string{"builtin"}, outputFile))
		zr, err := zip.OpenReader(outputFile)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, zr.Close()) })
		require.Len(t, zr.File, 1)
		var doc pkgdoc.PkgDoc
		require.NoError(t, json.Unmarshal(readGeneratedFile(t, zr, "builtin.pkgdoc"), &doc))
		assert.Equal(t, "builtin", doc.Path)
		assert.Equal(t, "builtin", doc.Name)
		assert.NotEmpty(t, doc.Funcs["len"])
		assert.NotEmpty(t, doc.Consts["true"])
		require.Contains(t, doc.Types, "int")
		assert.NotEmpty(t, doc.Types["int"].Doc)
	})

	t.Run("Empty", func(t *testing.T) {
		outputFile := filepath.Join(t.TempDir(), "pkgdata.zip")
		require.NoError(t, generate(nil, outputFile))
		zr, err := zip.OpenReader(outputFile)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, zr.Close()) })
		assert.Empty(t, zr.File)
	})

	t.Run("MissingOutputDirectory", func(t *testing.T) {
		outputFile := filepath.Join(t.TempDir(), "missing", "pkgdata.zip")
		assert.ErrorIs(t, generate(nil, outputFile), fs.ErrNotExist)
	})

	t.Run("InvalidPackage", func(t *testing.T) {
		const pkgPath = "example.com/invalid"
		t.Chdir(t.TempDir())
		require.NoError(t, os.WriteFile("go.mod", []byte("module "+pkgPath+"\n\ngo 1.25.0\n"), 0o644))
		require.NoError(t, os.WriteFile("invalid.go", []byte("package invalid\nconst Value int = \"invalid\"\n"), 0o644))
		err := generate([]string{pkgPath}, "pkgdata.zip")
		require.ErrorContains(t, err, "failed to execute go command")
		assert.ErrorContains(t, err, "cannot use")
		assert.NoFileExists(t, "pkgdata.zip")
	})
}

func generatedNamedType(t *testing.T, pkg *gotypes.Package, name string) *gotypes.Named {
	t.Helper()

	obj, ok := pkg.Scope().Lookup(name).(*gotypes.TypeName)
	require.True(t, ok, name)
	named, ok := obj.Type().(*gotypes.Named)
	require.True(t, ok, name)
	return named
}

func readGeneratedFile(t *testing.T, zr *zip.ReadCloser, name string) []byte {
	t.Helper()

	rc, err := zr.Open(name)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, rc.Close()) })
	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	return data
}
