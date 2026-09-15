package server

import (
	"fmt"
	gotypes "go/types"
	"path"
	"strings"
	"sync"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/analysis/ast/astutil"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
	"github.com/qiniu/x/errors"
)

// errNoMainSpxFile is the error returned when no valid main.spx file is found
// in the main package while compiling.
var errNoMainSpxFile = errors.New("no valid main.spx file found in main package")

// compileResult contains the compile results and additional information from
// the compile process.
type compileResult struct {
	definitionContext
	diagnosticResult

	// mainSpxFile is the main.spx file path.
	mainSpxFile string

	// spxSpriteTypes stores the spx sprite types.
	spxSpriteTypes map[gotypes.Type]struct{}

	// spxResourceSet is the set of spx resources.
	spxResourceSet SpxResourceSet

	// spxResourceRefs stores spx resource references.
	spxResourceRefs []SpxResourceRef

	// seenSpxResourceRefs stores already seen spx resource references to avoid
	// duplicates.
	seenSpxResourceRefs map[SpxResourceRef]struct{}

	// spxSpriteResourceAutoBindings stores spx sprite resource auto-bindings.
	spxSpriteResourceAutoBindings map[gotypes.Object]struct{}
}

// newCompileResult creates a new [compileResult].
func newCompileResult(proj *xgo.Project, lookupPkgDoc func(string) (*pkgdoc.PkgDoc, error)) *compileResult {
	return &compileResult{
		definitionContext: definitionContext{
			proj:         proj,
			enumInfo:     &enumInfo{},
			lookupPkgDoc: lookupPkgDoc,
		},
		spxSpriteTypes:                make(map[gotypes.Type]struct{}),
		spxSpriteResourceAutoBindings: make(map[gotypes.Object]struct{}),
		diagnosticResult:              newDiagnosticResult(),
	}
}

// isInSpxEventHandler checks if the given position is inside an spx event
// handler callback.
func (r *compileResult) isInSpxEventHandler(pos token.Pos) bool {
	astPkg, _ := r.proj.ASTPackage()
	astFile := xgoutil.PosASTFile(r.proj.Fset, astPkg, pos)
	if astFile == nil {
		return false
	}
	typeInfo, _ := r.proj.TypeInfo()
	if typeInfo == nil {
		return false
	}

	var isIn bool
	for node := range xgoutil.PathEnclosingIntervalNodes(astFile, pos-1, pos, false) {
		callExpr, ok := node.(*ast.CallExpr)
		if !ok || len(callExpr.Args) == 0 {
			continue
		}
		funcIdent, ok := callExpr.Fun.(*ast.Ident)
		if !ok {
			continue
		}
		funcObj := typeInfo.ObjectOf(funcIdent)
		if !IsInSpxPkg(funcObj) {
			continue
		}
		isIn = IsSpxEventHandlerFuncName(funcIdent.Name)
		if isIn {
			break
		}
	}
	return isIn
}

// spxResourceRefAtPosition returns the smallest resource reference containing
// position and its source file, including the position immediately after its
// source text.
func (r *compileResult) spxResourceRefAtPosition(position token.Position) (*SpxResourceRef, *ast.File) {
	var (
		bestRef      *SpxResourceRef
		bestFile     *ast.File
		bestNodeSpan int
	)
	fset := r.proj.Fset
	for _, ref := range r.spxResourceRefs {
		nodePos := fset.PositionFor(ref.Node.Pos(), false)
		if nodePos.Filename != position.Filename {
			continue
		}
		astFile := sourceASTFile(r.proj, ref.Node.Pos())
		if astFile == nil {
			continue
		}
		nodeEnd := fset.PositionFor(resourceNodeEnd(fset, astFile, ref.Node), false)
		if position.Line < nodePos.Line || position.Line > nodeEnd.Line ||
			position.Line == nodePos.Line && position.Column < nodePos.Column ||
			position.Line == nodeEnd.Line && position.Column > nodeEnd.Column {
			continue
		}

		nodeSpan := nodeEnd.Offset - nodePos.Offset
		if bestRef == nil || nodeSpan < bestNodeSpan {
			bestRef = &ref
			bestFile = astFile
			bestNodeSpan = nodeSpan
		}
	}
	return bestRef, bestFile
}

// hasSpxSpriteType reports whether the given type is an spx sprite type.
func (r *compileResult) hasSpxSpriteType(typ gotypes.Type) bool {
	_, ok := r.spxSpriteTypes[typ]
	return ok
}

// hasSpxSpriteResourceAutoBinding reports whether the given object is an spx
// resource auto-binding.
func (r *compileResult) hasSpxSpriteResourceAutoBinding(obj gotypes.Object) bool {
	_, ok := r.spxSpriteResourceAutoBindings[obj]
	return ok
}

// addSpxResourceRef adds an spx resource reference to the compile result.
func (r *compileResult) addSpxResourceRef(ref SpxResourceRef) {
	if r.seenSpxResourceRefs == nil {
		r.seenSpxResourceRefs = make(map[SpxResourceRef]struct{})
	}

	if _, ok := r.seenSpxResourceRefs[ref]; ok {
		return
	}
	r.seenSpxResourceRefs[ref] = struct{}{}

	r.spxResourceRefs = append(r.spxResourceRefs, ref)
}

// compile compiles spx source files and returns compile result. It uses cached
// result if available.
func (s *Server) compile() (*compileResult, error) {
	// NOTE(xsw): don't create a snapshot
	snapshot := s.workspaceRootFS // .Snapshot()

	// TODO(wyvern): remove this once we have a better way to update files.
	snapshot.UpdateFiles(s.fileMapGetter())
	return s.compileAt(snapshot)
}

// compileAt compiles spx source files at the given snapshot and returns the
// compile result.
func (s *Server) compileAt(snapshot *xgo.Project) (*compileResult, error) {
	var hasSpxFile bool
	for file := range snapshot.Files() {
		if path.Ext(file) == ".spx" {
			hasSpxFile = true
			break
		}
	}
	if !hasSpxFile {
		return nil, errNoMainSpxFile
	}

	result := newCompileResult(snapshot, s.lookupPkgDoc)
	astPkg, err := s.collectPackageSyntaxDiagnostics(snapshot, &result.diagnosticResult)
	if err != nil {
		return nil, err
	}
	for filename, astFile := range astPkg.Files {
		if path.Ext(filename) != ".spx" {
			continue
		}
		if astFile.Name.Name != "main" && astFile.Pos().IsValid() {
			result.addDiagnostics(s.toDocumentURI(filename), Diagnostic{
				Severity: SeverityError,
				Range:    RangeForASTFileNode(result.proj, astFile, astFile.Name),
				Message:  s.translate("package name must be main"),
			})
			continue
		}

		if path.Base(filename) == "main.spx" {
			result.mainSpxFile = filename
		}
	}
	if result.mainSpxFile == "" {
		if len(result.diagnostics) == 0 {
			return nil, errNoMainSpxFile
		}
		return result, nil
	}

	typeInfo := s.collectTypeDiagnostics(snapshot, &result.diagnosticResult)
	result.enumInfo = newEnumInfo(astPkg, typeInfo)
	pkg := typeInfo.Pkg

	for file := range snapshot.Files() {
		if file == "main.spx" {
			// Skip the main.spx file, as it is not a sprite file.
			continue
		}
		if path.Ext(file) != ".spx" {
			continue
		}

		spriteName := strings.TrimSuffix(path.Base(file), ".spx")
		obj := pkg.Scope().Lookup(spriteName)
		if obj != nil {
			named, ok := xgoutil.DerefType(obj.Type()).(*gotypes.Named)
			if ok {
				result.spxSpriteTypes[named] = struct{}{}
			}
		}
	}

	s.inspectForSpxResourceSet(snapshot, result)
	s.inspectForSpxResourceRefs(result)
	s.inspectDiagnosticsAnalyzers(snapshot, &result.diagnosticResult, spxDiagnosticPass(result))

	return result, nil
}

// compileAndGetASTFileForDocumentURI handles common compilation and file
// retrieval logic for a given document URI. The returned astFile is probably
// nil even if the compilation succeeded.
func (s *Server) compileAndGetASTFileForDocumentURI(uri DocumentURI) (result *compileResult, spxFile string, astFile *ast.File, err error) {
	spxFile, err = s.fromDocumentURI(uri)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to get file path from document URI %q: %w", uri, err)
	}
	if path.Ext(spxFile) != ".spx" {
		return nil, "", nil, fmt.Errorf("file %q does not have .spx extension", spxFile)
	}
	result, err = s.compile()
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to compile: %w", err)
	}
	if astPkg, _ := result.proj.ASTPackage(); astPkg != nil {
		astFile = astPkg.Files[spxFile]
	}
	return
}

// inspectForSpxResourceSet inspects for spx resource set in main.spx.
func (s *Server) inspectForSpxResourceSet(snapshot *xgo.Project, result *compileResult) {
	spxResourceSet, err := NewSpxResourceSet(snapshot)
	if err != nil {
		documentURI := s.toDocumentURI(result.mainSpxFile)
		result.addDiagnostics(documentURI, Diagnostic{
			Severity: SeverityError,
			Message:  s.translate(fmt.Sprintf("failed to create spx resource set: %v", err)),
		})
		return
	}
	result.spxResourceSet = *spxResourceSet
}

// inspectForSpxResourceRefs inspects for spx resource references in the code.
func (s *Server) inspectForSpxResourceRefs(result *compileResult) {
	s.inspectForAutoBindingSpxResources(result)

	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return
	}
	astPkg, _ := result.proj.ASTPackage()
	resourceExprs := make(map[ast.Expr]struct{})

	// Declarations, assignments, and returns supply the target type without
	// propagating it into unrelated subexpressions such as call arguments.
	inspectValue := func(expr ast.Expr, typ gotypes.Type) {
		getSpriteContext := func() *SpxSpriteResource {
			return spxSpriteResourceForFile(result, result.proj.Fset.PositionFor(expr.Pos(), false).Filename)
		}
		s.inspectSpxResourceRefForTypeAtExpr(result, expr, xgoutil.DerefType(typ), getSpriteContext, resourceExprs)
	}
	for expr, typ := range valueExprTypes(astPkg, typeInfo) {
		inspectValue(expr, typ)
	}

	// Check call arguments from the AST, since calls containing invalid or
	// partially typed arguments may be absent from typeInfo.Types.
	if astPkg != nil {
		for _, file := range astPkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				call := callExprFromNode(n)
				if call == nil {
					return true
				}
				s.inspectSpxResourceRefsForCallExpr(result, typeInfo, call, resourceExprs)
				return true
			})
		}
	}

	// Preserve references whose resource type is carried by the expression
	// itself, unless an assignment, return, or call supplies a resource type.
	for expr, tv := range typeInfo.Types {
		if expr == nil || !expr.Pos().IsValid() || tv.IsType() || tv.Type == nil {
			continue
		}
		if _, ok := resourceExprs[expr]; ok {
			continue
		}
		switch expr.(type) {
		case *ast.BasicLit, *ast.Ident:
			switch canonicalSpxResourceNameType(xgoutil.DerefType(tv.Type)) {
			case GetSpxBackdropNameType(), GetSpxSpriteNameType(), GetSpxSoundNameType(), GetSpxWidgetNameType():
				inspectValue(expr, tv.Type)
			}
		}
	}
}

// inspectSpxResourceRefsForCallExpr inspects spx resource references in call
// arguments.
func (s *Server) inspectSpxResourceRefsForCallExpr(
	result *compileResult, typeInfo *types.Info, call *ast.CallExpr, resourceExprs map[ast.Expr]struct{},
) {
	if len(call.Args) == 0 && len(call.Kwargs) == 0 {
		return
	}

	getSpriteContext := sync.OnceValue(func() *SpxSpriteResource {
		return s.resolveSpxSpriteContextFromCallExpr(result, call)
	})
	for expr, typ := range callArgValueTypes(typeInfo, call) {
		s.inspectSpxResourceRefForTypeAtExpr(result, expr, typ, getSpriteContext, resourceExprs)
	}
}

// inspectForAutoBindingSpxResources inspects for auto-binding spx resources and
// their references.
func (s *Server) inspectForAutoBindingSpxResources(result *compileResult) {
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return
	}

	gameObj := typeInfo.Pkg.Scope().Lookup("Game")
	if gameObj == nil {
		return
	}
	gameType, ok := gameObj.Type().(*gotypes.Named)
	if !ok || !xgoutil.IsNamedStructType(gameType) {
		return
	}
	for structMember := range xgoutil.StructMembers(gameType) {
		field, ok := structMember.Member.(*gotypes.Var)
		if !ok {
			continue
		}
		fieldType, ok := xgoutil.DerefType(field.Type()).(*gotypes.Named)
		if !ok {
			continue
		}
		if fieldType == GetSpxSpriteType() || result.hasSpxSpriteType(fieldType) {
			result.spxSpriteResourceAutoBindings[structMember.Member] = struct{}{}
		}
	}
	for ident, obj := range typeInfo.Uses {
		if result.hasSpxSpriteResourceAutoBinding(obj) && !ident.Implicit() {
			result.addSpxResourceRef(SpxResourceRef{
				ID:   SpxSpriteResourceID{SpriteName: obj.Name()},
				Kind: SpxResourceRefKindAutoBindingReference,
				Node: ident,
			})
		}
	}
}

// resolveSpxSpriteContextFromCallExpr resolves the sprite context from a call expression.
func (s *Server) resolveSpxSpriteContextFromCallExpr(result *compileResult, callExpr *ast.CallExpr) *SpxSpriteResource {
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}

	funcType := typeInfo.TypeOf(callExpr.Fun)
	if !xgoutil.IsValidType(funcType) {
		return nil
	}
	funcSig, ok := funcType.(*gotypes.Signature)
	if !ok {
		return nil
	}
	funcSigRecv := funcSig.Recv()
	if funcSigRecv == nil {
		return nil
	}
	switch xgoutil.DerefType(funcSigRecv.Type()) {
	case GetSpxSpriteType(), GetSpxSpriteImplType():
	default:
		return nil
	}

	return spxSpriteResourceForCall(result, callExpr)
}

// inspectSpxResourceRefForTypeAtExpr inspects an spx resource reference for a
// given type at an expression. It records recognized resource expressions so
// their contextual type takes precedence over their intrinsic type.
func (s *Server) inspectSpxResourceRefForTypeAtExpr(
	result *compileResult, expr ast.Expr, typ gotypes.Type,
	getSpriteContext func() *SpxSpriteResource, resourceExprs map[ast.Expr]struct{},
) {
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return
	}
	expr = astutil.Unparen(expr)
	name, ok := xgoutil.StringLitOrConstValue(expr, typeInfo.Types[expr])
	if !ok {
		return
	}
	kind := SpxResourceRefKindStringLiteral
	if _, ok := expr.(*ast.Ident); ok {
		kind = SpxResourceRefKindConstantReference
	}

	typ = canonicalSpxResourceNameType(typ)
	if typ == nil {
		return
	}
	resourceExprs[expr] = struct{}{}

	var id SpxResourceID
	switch typ {
	case GetSpxBackdropNameType():
		id = SpxBackdropResourceID{BackdropName: name}
	case GetSpxSpriteNameType():
		id = SpxSpriteResourceID{SpriteName: name}
	case GetSpxSpriteCostumeNameType():
		sprite := getSpriteContext()
		if sprite == nil {
			return
		}
		id = SpxSpriteCostumeResourceID{SpriteName: sprite.Name, CostumeName: name}
	case GetSpxSpriteAnimationNameType():
		sprite := getSpriteContext()
		if sprite == nil {
			return
		}
		id = SpxSpriteAnimationResourceID{SpriteName: sprite.Name, AnimationName: name}
	case GetSpxSoundNameType():
		id = SpxSoundResourceID{SoundName: name}
	case GetSpxWidgetNameType():
		id = SpxWidgetResourceID{WidgetName: name}
	}
	s.inspectSpxResourceRef(result, SpxResourceRef{ID: id, Kind: kind, Node: expr})
}

// inspectSpxResourceRef records a resolved string resource reference and
// diagnoses empty names or missing resources without consulting SDK types.
func (s *Server) inspectSpxResourceRef(result *compileResult, ref SpxResourceRef) {
	var resourceType, emptyResourceType, spriteName string
	switch id := ref.ID.(type) {
	case SpxBackdropResourceID:
		resourceType = "backdrop"
	case SpxSpriteResourceID:
		resourceType = "sprite"
	case SpxSpriteCostumeResourceID:
		resourceType, emptyResourceType, spriteName = "costume", "sprite costume", id.SpriteName
	case SpxSpriteAnimationResourceID:
		resourceType, emptyResourceType, spriteName = "animation", "sprite animation", id.SpriteName
	case SpxSoundResourceID:
		resourceType = "sound"
	case SpxWidgetResourceID:
		resourceType = "widget"
	}
	if emptyResourceType == "" {
		emptyResourceType = resourceType
	}
	if ref.ID.Name() == "" {
		s.addEmptySpxResourceNameDiagnostic(result, ref.Node, emptyResourceType)
		return
	}
	result.addSpxResourceRef(ref)
	if !result.spxResourceSet.Contains(ref.ID) {
		s.addSpxResourceNotFoundDiagnostic(result, ref.Node, resourceType, ref.ID.Name(), spriteName)
	}
}

// addEmptySpxResourceNameDiagnostic adds a diagnostic for empty spx resource name.
func (s *Server) addEmptySpxResourceNameDiagnostic(result *compileResult, expr ast.Node, resourceType string) {
	astFile := sourceASTFile(result.proj, expr.Pos())
	if astFile == nil {
		return
	}
	uri := s.toDocumentURI(result.proj.Fset.File(expr.Pos()).Name())
	result.addDiagnostics(uri, Diagnostic{
		Severity: SeverityError,
		Range:    resourceRange(result.proj, astFile, expr),
		Message:  s.translate(fmt.Sprintf("%s resource name cannot be empty", resourceType)),
	})
}

// addSpxResourceNotFoundDiagnostic adds a diagnostic for spx resource not found.
func (s *Server) addSpxResourceNotFoundDiagnostic(result *compileResult, expr ast.Node, resourceType, resourceName, contextSpriteName string) {
	astFile := sourceASTFile(result.proj, expr.Pos())
	if astFile == nil {
		return
	}
	message := fmt.Sprintf("%s resource %q not found", resourceType, resourceName)
	if contextSpriteName != "" {
		message = fmt.Sprintf("%s in sprite %q", message, contextSpriteName)
	}
	uri := s.toDocumentURI(result.proj.Fset.File(expr.Pos()).Name())
	result.addDiagnostics(uri, Diagnostic{
		Severity: SeverityError,
		Range:    resourceRange(result.proj, astFile, expr),
		Message:  s.translate(message),
	})
}
