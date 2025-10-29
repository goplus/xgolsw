package spx

import (
	stdErrors "errors"
	"fmt"
	"go/token"
	"path"
	"strings"

	"github.com/goplus/gogen"
	xgoscanner "github.com/goplus/xgo/scanner"
	"github.com/goplus/xgo/x/typesutil"
	"github.com/goplus/xgolsw/internal/classfile"
	"github.com/goplus/xgolsw/xgo"
	xerrors "github.com/qiniu/x/errors"
)

// ProviderID identifies the spx provider.
const ProviderID = classfile.ProviderID("spx")

// NewProvider constructs a new spx provider instance.
func NewProvider() classfile.Provider {
	return provider{}
}

type provider struct{}

func (provider) ID() classfile.ProviderID { return ProviderID }

func (provider) Supports(path string) bool {
	return strings.HasSuffix(path, ".spx")
}

func (provider) Build(ctx *classfile.Context) (*classfile.Snapshot, error) {
	if ctx == nil || ctx.Project == nil {
		return nil, fmt.Errorf("project is nil")
	}

	translate := ctx.Translator
	if translate == nil {
		translate = func(s string) string { return s }
	}

	proj := ctx.Project
	fset := proj.Fset

	spxFiles := collectSpxFiles(proj)
	diagnostics := make([]typesutil.Error, 0)

	if len(spxFiles) == 0 {
		diagnostics = append(diagnostics, typesutil.Error{Fset: fset, Msg: translate("no spx files found")})
	}

	var mainFile string
	for _, spxFile := range spxFiles {
		fileMain, errs := analyzeFile(proj, spxFile, translate)
		if fileMain {
			mainFile = spxFile
		}
		diagnostics = append(diagnostics, errs...)
	}

	var resourceSet *ResourceSet
	if mainFile == "" {
		diagnostics = append(diagnostics, typesutil.Error{Fset: fset, Msg: translate("no valid main.spx file found in main package")})
	} else {
		set, errs := buildResources(proj, translate)
		diagnostics = append(diagnostics, errs...)
		resourceSet = set
	}

	refs, refDiagnostics := collectResourceRefs(proj, resourceSet, translate)
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

const defaultResourceRoot = "assets"

func collectSpxFiles(proj *xgo.Project) []string {
	var files []string
	for path := range proj.Files() {
		if strings.HasSuffix(path, ".spx") {
			files = append(files, path)
		}
	}
	return files
}

func analyzeFile(proj *xgo.Project, spxFile string, translate func(string) string) (bool, []typesutil.Error) {
	var diagnostics []typesutil.Error
	astFile, err := proj.ASTFile(spxFile)
	if err != nil {
		diagnostics = append(diagnostics, convertParseError(proj.Fset, err, translate)...)
	}
	if astFile == nil {
		return false, diagnostics
	}

	if astFile.Name.Name != "main" && astFile.Pos().IsValid() {
		diagnostics = append(diagnostics, typesutil.Error{
			Fset: proj.Fset,
			Pos:  astFile.Name.Pos(),
			End:  astFile.Name.End(),
			Msg:  translate("package name must be main"),
		})
		return false, diagnostics
	}
	return path.Base(spxFile) == "main.spx", diagnostics
}

func convertParseError(fset *token.FileSet, err error, translate func(string) string) []typesutil.Error {
	var diagnostics []typesutil.Error

	var scannerErrs xgoscanner.ErrorList
	switch {
	case stdErrors.As(err, &scannerErrs):
		for _, e := range scannerErrs {
			diagnostics = append(diagnostics, typesutil.Error{Fset: fset, Msg: translate(e.Msg)})
		}
	default:
		var codeErr *gogen.CodeError
		if stdErrors.As(err, &codeErr) {
			diagnostics = append(diagnostics, typesutil.Error{Fset: fset, Pos: codeErr.Pos, End: codeErr.End, Msg: translate(codeErr.Error())})
		} else {
			diagnostics = append(diagnostics, typesutil.Error{Fset: fset, Msg: translate(fmt.Sprintf("failed to parse spx file: %v", err))})
		}
	}
	return diagnostics
}

func buildResources(proj *xgo.Project, translate func(string) string) (*ResourceSet, []typesutil.Error) {
	set, err := NewResourceSet(proj, defaultResourceRoot)
	if err != nil {
		return nil, []typesutil.Error{{Fset: proj.Fset, Msg: translate(fmt.Sprintf("failed to create spx resource set: %v", err))}}
	}
	return set, nil
}

func collectTypeErrors(proj *xgo.Project) []typesutil.Error {
	_, err := proj.TypeInfo()
	if err == nil {
		return nil
	}

	diagnostics := make([]typesutil.Error, 0)
	var addErr func(error)
	addErr = func(e error) {
		if e == nil {
			return
		}
		switch typed := e.(type) {
		case typesutil.Error:
			diagnostics = append(diagnostics, typed)
		case xerrors.List:
			for _, item := range typed {
				addErr(item)
			}
		default:
			var typeErr typesutil.Error
			if stdErrors.As(e, &typeErr) {
				diagnostics = append(diagnostics, typeErr)
			}
		}
	}
	addErr(err)
	return diagnostics
}
