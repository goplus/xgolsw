package spx

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/goplus/xgo/scanner"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgo/x/typesutil"
	"github.com/goplus/xgolsw/internal/classfile"
	"github.com/goplus/xgolsw/xgo"
	xerrors "github.com/qiniu/x/errors"
)

const (
	// ProviderID identifies the spx provider.
	ProviderID = classfile.ProviderID("spx")

	// ResourceSetKey stores the resolved [ResourceSet] in a snapshot's resource index.
	ResourceSetKey = "spx/resourceSet"

	// ResourceRootKey stores the root directory used to resolve spx resources.
	ResourceRootKey = "spx/resourceRoot"

	// MainFileKey stores the path to the primary main.spx file, if present.
	MainFileKey = "spx/mainFile"

	// ResourceRefsKey stores collected spx resource references in the snapshot.
	ResourceRefsKey = "spx/resourceRefs"
)

// NewProvider constructs a new spx provider instance.
func NewProvider() classfile.Provider {
	return provider{}
}

// provider implements the spx-specific [classfile.Provider].
type provider struct{}

// ID implements [classfile.Provider].
func (provider) ID() classfile.ProviderID {
	return ProviderID
}

// Supports implements [classfile.Provider].
func (provider) Supports(path string) bool {
	return strings.HasSuffix(path, ".spx")
}

// Build implements [classfile.Provider].
func (provider) Build(buildCtx *classfile.BuildContext) (*classfile.Snapshot, error) {
	if buildCtx == nil || buildCtx.Project == nil {
		return nil, fmt.Errorf("nil project")
	}

	proj := buildCtx.Project
	fset := proj.Fset

	spxFiles := collectSpxFiles(proj)
	diagnostics := make([]typesutil.Error, 0)

	if len(spxFiles) == 0 {
		diagnostics = append(diagnostics, typesutil.Error{Fset: fset, Msg: "no spx files found"})
	}

	var mainFile string
	for _, spxFile := range spxFiles {
		fileMain, errs := analyzeFile(proj, spxFile)
		if fileMain {
			mainFile = spxFile
		}
		diagnostics = append(diagnostics, errs...)
	}

	var resourceSet *ResourceSet
	if mainFile == "" {
		diagnostics = append(diagnostics, typesutil.Error{Fset: fset, Msg: "no valid main.spx file found in main package"})
	} else {
		set, errs := buildResources(proj)
		diagnostics = append(diagnostics, errs...)
		resourceSet = set
	}

	refs, refDiagnostics := collectResourceRefs(proj, resourceSet, func(s string) string { return s })
	diagnostics = append(diagnostics, refDiagnostics...)
	diagnostics = append(diagnostics, collectTypeErrors(proj)...)

	resources := &classfile.ResourceIndex{}
	if resourceSet != nil {
		resources.Set(ResourceSetKey, resourceSet)
		resources.Set(ResourceRootKey, defaultResourceRoot)
	}
	if len(refs) > 0 {
		resources.Set(ResourceRefsKey, refs)
	}
	if mainFile != "" {
		resources.Set(MainFileKey, mainFile)
	}

	snapshot := &classfile.Snapshot{
		Provider:    ProviderID,
		Diagnostics: diagnostics,
		Resources:   resources,
		Symbols:     &classfile.SymbolIndex{},
	}
	return snapshot, nil
}

// defaultResourceRoot is the fallback asset directory for spx resources.
const defaultResourceRoot = "assets"

// collectSpxFiles gathers every .spx file path registered in proj.
func collectSpxFiles(proj *xgo.Project) []string {
	var files []string
	for path := range proj.Files() {
		if strings.HasSuffix(path, ".spx") {
			files = append(files, path)
		}
	}
	return files
}

// analyzeFile parses a single spx file and reports whether it is main.spx.
func analyzeFile(proj *xgo.Project, spxFile string) (bool, []typesutil.Error) {
	var diagnostics []typesutil.Error
	astFile, err := proj.ASTFile(spxFile)
	if err != nil {
		var errorList scanner.ErrorList
		if errors.As(err, &errorList) {
			for _, err := range errorList {
				start, end := scannerErrorRange(proj.Fset, err)
				diagnostics = append(diagnostics, typesutil.Error{
					Fset: proj.Fset,
					Pos:  start,
					End:  end,
					Msg:  err.Msg,
				})
			}
		} else {
			diagnostics = append(diagnostics, typesutil.Error{
				Fset: proj.Fset,
				Msg:  fmt.Sprintf("failed to parse spx file: %v", err),
			})
		}
	}
	if astFile == nil {
		return false, diagnostics
	}

	if astFile.Name.Name != "main" && astFile.Pos().IsValid() {
		diagnostics = append(diagnostics, typesutil.Error{
			Fset: proj.Fset,
			Pos:  astFile.Name.Pos(),
			End:  astFile.Name.End(),
			Msg:  "package name must be main",
		})
		return false, diagnostics
	}
	return path.Base(spxFile) == "main.spx", diagnostics
}

// scannerErrorRange resolves the start and end positions for the provided
// [scanner.Error]. It returns [token.NoPos] for both values when the location
// cannot be determined.
func scannerErrorRange(fset *token.FileSet, err *scanner.Error) (start token.Pos, end token.Pos) {
	if err == nil {
		return token.NoPos, token.NoPos
	}
	position := err.Pos
	if position.Filename == "" {
		return token.NoPos, token.NoPos
	}

	var file *token.File
	fset.Iterate(func(f *token.File) bool {
		if f.Name() == position.Filename {
			file = f
			return false
		}
		return true
	})
	if file == nil {
		return token.NoPos, token.NoPos
	}

	offset := position.Offset
	if offset < 0 {
		if position.Line > 0 {
			lineStart := file.LineStart(position.Line)
			if lineStart.IsValid() {
				base := file.Offset(lineStart)
				col := max(position.Column, 1)
				offset = base + col - 1
			}
		}
	}
	if offset < 0 || offset > file.Size() {
		return token.NoPos, token.NoPos
	}
	pos := file.Pos(offset)
	return pos, pos
}

// buildResources constructs the spx resource set and associated diagnostics.
func buildResources(proj *xgo.Project) (*ResourceSet, []typesutil.Error) {
	set, err := NewResourceSet(proj, defaultResourceRoot)
	if err != nil {
		return nil, []typesutil.Error{{Fset: proj.Fset, Msg: fmt.Sprintf("failed to create spx resource set: %v", err)}}
	}
	return set, nil
}

// collectTypeErrors extracts type-checking diagnostics from the project.
func collectTypeErrors(proj *xgo.Project) []typesutil.Error {
	_, err := proj.TypeInfo()
	if err == nil {
		return nil
	}

	var diagnostics []typesutil.Error
	var addErr func(error)
	addErr = func(err error) {
		if err == nil {
			return
		}
		switch err := err.(type) {
		case typesutil.Error:
			diagnostics = append(diagnostics, err)
		case xerrors.List:
			for _, err := range err {
				addErr(err)
			}
		default:
			var typeErr typesutil.Error
			if errors.As(err, &typeErr) {
				diagnostics = append(diagnostics, typeErr)
			}
		}
	}
	addErr(err)
	return diagnostics
}
