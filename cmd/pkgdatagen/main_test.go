package main

import (
	"archive/zip"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	_ "github.com/goplus/spx/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPkgDataGen(t *testing.T) {
	if os.Getenv("XGOLSW_TEST_PKGDATAGEN") == "1" {
		os.Args = append([]string{"pkgdatagen"}, flag.Args()...)
		flag.CommandLine = flag.NewFlagSet("pkgdatagen", flag.ExitOnError)
		main()
		return
	}

	for _, tt := range []struct {
		name       string
		args       []string
		pkgPaths   []string
		outputFile string
	}{
		{name: "NoDefaultsEmpty", args: []string{"-no-defaults"}},
		{
			name: "NoDefaultsWithExplicitPackages", args: []string{"-no-defaults", "builtin", "errors", "builtin", "errors"},
			pkgPaths: []string{"builtin", "errors"}, outputFile: "selected.zip",
		},
		{
			name: "DefaultsWithAdditionalPackages", args: []string{"builtin", "math/rand/v2", "math/rand/v2"},
			pkgPaths: append(slices.Clone(defaultPkgPaths), "math/rand/v2"), outputFile: "defaults.zip",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			args, flags := os.Args, flag.CommandLine
			t.Cleanup(func() {
				os.Args, flag.CommandLine = args, flags
			})
			os.Args = []string{"pkgdatagen"}
			outputFile := tt.outputFile
			if outputFile == "" {
				t.Chdir(t.TempDir())
				outputFile = "pkgdata.zip"
			} else {
				outputFile = filepath.Join(t.TempDir(), outputFile)
				os.Args = append(os.Args, "-o", outputFile)
			}
			os.Args = append(os.Args, tt.args...)
			flag.CommandLine = flag.NewFlagSet("pkgdatagen", flag.ExitOnError)
			main()

			zr, err := zip.OpenReader(outputFile)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, zr.Close()) })
			var names, want []string
			for _, f := range zr.File {
				names = append(names, f.Name)
			}
			for _, pkgPath := range tt.pkgPaths {
				if pkgPath != "builtin" {
					want = append(want, pkgPath+".pkgexport")
				}
				want = append(want, pkgPath+".pkgdoc")
			}
			assert.ElementsMatch(t, want, names)
		})
	}

	for _, tt := range []struct {
		name     string
		args     []string
		exitCode int
		message  string
	}{
		{name: "MissingPackage", args: []string{"-no-defaults", "./missing"}, exitCode: 1, message: "failed to generate package data: failed to load package"},
		{name: "InvalidFlag", args: []string{"-unknown"}, exitCode: 2, message: "flag provided but not defined: -unknown"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			outputDir := t.TempDir()
			outputFile := filepath.Join(outputDir, "pkgdata.zip")
			const original = "previous package data"
			require.NoError(t, os.WriteFile(outputFile, []byte(original), 0o644))
			executable, err := os.Executable()
			require.NoError(t, err)
			args := append([]string{"-test.run=^TestPkgDataGen$", "--", "-o", outputFile}, tt.args...)
			cmd := exec.CommandContext(t.Context(), executable, args...)
			cmd.Dir = outputDir
			cmd.Env = append(os.Environ(), "XGOLSW_TEST_PKGDATAGEN=1")
			output, err := cmd.CombinedOutput()
			var exitErr *exec.ExitError
			require.ErrorAs(t, err, &exitErr)
			assert.Equal(t, tt.exitCode, exitErr.ExitCode(), string(output))
			assert.Contains(t, string(output), tt.message)
			data, err := os.ReadFile(outputFile)
			require.NoError(t, err)
			assert.Equal(t, original, string(data))
		})
	}
}

func TestGenerateSpxEnginePackage(t *testing.T) {
	pkgPath := "github.com/goplus/spx/v3/pkg/spx/pkg/engine"
	outputFile := filepath.Join(t.TempDir(), "pkgdata.zip")
	require.NoError(t, generate([]string{pkgPath}, outputFile))

	zipReader, err := zip.OpenReader(outputFile)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, zipReader.Close()) })

	fileNames := make([]string, 0, len(zipReader.File))
	for _, file := range zipReader.File {
		fileNames = append(fileNames, file.Name)
	}
	assert.Contains(t, fileNames, pkgPath+".pkgexport")
	assert.Contains(t, fileNames, pkgPath+".pkgdoc")
}
