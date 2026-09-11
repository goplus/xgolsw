package server

import (
	"fmt"
	gotypes "go/types"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
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

// spxResourceRefAtPosition returns the spx resource reference at the given position.
func (r *compileResult) spxResourceRefAtPosition(position token.Position) *SpxResourceRef {
	var (
		bestRef      *SpxResourceRef
		bestNodeSpan int
	)
	fset := r.proj.Fset
	for _, ref := range r.spxResourceRefs {
		nodePos := fset.Position(ref.Node.Pos())
		nodeEnd := fset.Position(ref.Node.End())
		if nodePos.Filename != position.Filename ||
			position.Line != nodePos.Line ||
			position.Column < nodePos.Column ||
			position.Column > nodeEnd.Column {
			continue
		}

		nodeSpan := nodeEnd.Column - nodePos.Column
		if bestRef == nil || nodeSpan < bestNodeSpan {
			bestRef = &ref
			bestNodeSpan = nodeSpan
		}
	}
	return bestRef
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
	returnTypes := spxResourceReturnTypes(astPkg, typeInfo)

	// Check all identifier definitions.
	for ident, obj := range typeInfo.Defs {
		if ident == nil || !ident.Pos().IsValid() || ident.Implicit() || obj == nil {
			continue
		}

		switch obj.(type) {
		case *gotypes.Const, *gotypes.Var:
			if ident.Obj == nil {
				break
			}
			valueSpec, ok := ident.Obj.Decl.(*ast.ValueSpec)
			if !ok {
				break
			}
			idx := slices.Index(valueSpec.Names, ident)
			if idx < 0 || idx >= len(valueSpec.Values) {
				break
			}
			expr := valueSpec.Values[idx]

			s.inspectSpxResourceRefForTypeAtExpr(result, expr, xgoutil.DerefType(obj.Type()), nil)
		}
	}

	// Check all type-checked expressions.
	for expr, tv := range typeInfo.Types {
		if expr == nil || !expr.Pos().IsValid() || tv.IsType() || tv.Type == nil {
			continue
		}

		switch expr := expr.(type) {
		case *ast.BasicLit:
			if expr.Kind == token.STRING {
				if returnType := returnTypes[expr]; returnType != nil {
					getSpriteContext := sync.OnceValue(func() *SpxSpriteResource {
						spxFileBaseName := path.Base(xgoutil.NodeFilename(result.proj.Fset, expr))
						if spxFileBaseName == "main.spx" {
							return nil
						}
						spriteName := strings.TrimSuffix(spxFileBaseName, ".spx")
						return result.spxResourceSet.Sprite(spriteName)
					})
					s.inspectSpxResourceRefForTypeAtExpr(result, expr, returnType, getSpriteContext)
				} else {
					s.inspectSpxResourceRefForTypeAtExpr(result, expr, xgoutil.DerefType(tv.Type), nil)
				}
			}
		case *ast.Ident:
			typ := xgoutil.DerefType(tv.Type)
			switch typ {
			case GetSpxBackdropNameType(),
				GetSpxSpriteNameType(),
				GetSpxSoundNameType(),
				GetSpxWidgetNameType():
				s.inspectSpxResourceRefForTypeAtExpr(result, s.resolveIdentifierToAssignedExpr(result, expr), typ, nil)
			}
		}
	}

	// Check call arguments from the AST, since calls containing invalid or
	// partially typed arguments may be absent from typeInfo.Types.
	if astPkg == nil {
		return
	}
	for _, file := range astPkg.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			call := callExprFromNode(n)
			if call == nil {
				return true
			}
			s.inspectSpxResourceRefsForCallExpr(result, typeInfo, call)
			return true
		})
	}
}

// inspectSpxResourceRefsForCallExpr inspects spx resource references in call
// arguments.
func (s *Server) inspectSpxResourceRefsForCallExpr(result *compileResult, typeInfo *types.Info, call *ast.CallExpr) {
	if len(call.Args) == 0 && len(call.Kwargs) == 0 {
		return
	}
	fun := xgoutil.FuncFromCallExpr(typeInfo, call)
	if fun == nil {
		return
	}
	mayHaveResourceParams := HasSpxResourceNameTypeParams(fun) || len(call.Kwargs) > 0 ||
		xgoutil.IsXGoOverloadableFunc(fun) || xgoutil.IsXGoOverloadedFuncName(fun.Name())
	if !mayHaveResourceParams {
		return
	}

	getSpriteContext := sync.OnceValue(func() *SpxSpriteResource {
		return s.resolveSpxSpriteContextFromCallExpr(result, call)
	})
	for resolvedArg := range resolvedCallExprArgs(result.proj, typeInfo, call) {
		if resolvedArg.ExpectedType == nil {
			continue
		}
		paramType := xgoutil.DerefType(resolvedArg.ExpectedType)

		if elts, ok := xgoCollectionLitElts(resolvedArg.Arg); ok {
			paramType = spxResourceNameValueType(resolvedArg.ExpectedType)
			for _, elt := range elts {
				s.inspectSpxResourceRefForTypeAtExpr(result, elt, paramType, getSpriteContext)
			}
		} else {
			s.inspectSpxResourceRefForTypeAtExpr(result, resolvedArg.Arg, paramType, getSpriteContext)
		}
	}
}

// xgoCollectionLitElts returns the flattened elements of an XGo collection
// literal.
func xgoCollectionLitElts(expr ast.Expr) ([]ast.Expr, bool) {
	switch expr := expr.(type) {
	case *ast.SliceLit:
		return expr.Elts, true
	case *ast.MatrixLit:
		return slices.Concat(expr.Elts...), true
	default:
		return nil, false
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

// resolveIdentifierToAssignedExpr resolves an identifier to its assigned
// expression by looking for assignment statements in the AST.
func (s *Server) resolveIdentifierToAssignedExpr(result *compileResult, ident *ast.Ident) ast.Expr {
	astPkg, _ := result.proj.ASTPackage()
	astFile := xgoutil.NodeASTFile(result.proj.Fset, astPkg, ident)
	if astFile == nil {
		return ident
	}

	var resolvedExpr ast.Expr = ident
	for node := range xgoutil.PathEnclosingIntervalNodes(astFile, ident.Pos(), ident.End(), false) {
		assignStmt, ok := node.(*ast.AssignStmt)
		if !ok {
			continue
		}

		idx := slices.IndexFunc(assignStmt.Lhs, func(lhs ast.Expr) bool {
			return lhs == ident
		})
		if idx < 0 || idx >= len(assignStmt.Rhs) {
			continue
		}
		resolvedExpr = assignStmt.Rhs[idx]
		break
	}
	return resolvedExpr
}

// spxResourceReturnTypes returns the expected SPX resource type for each
// string literal contained in a resource-typed return value.
func spxResourceReturnTypes(astPkg *ast.Package, typeInfo *types.Info) map[*ast.BasicLit]gotypes.Type {
	returnTypes := make(map[*ast.BasicLit]gotypes.Type)
	if astPkg == nil || typeInfo == nil {
		return returnTypes
	}

	var inspectResourceReturns func(ast.Node, *gotypes.Signature)
	inspectResourceReturns = func(root ast.Node, sig *gotypes.Signature) {
		if root == nil {
			return
		}
		ast.Inspect(root, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.FuncDecl:
				fun, _ := typeInfo.ObjectOf(node.Name).(*gotypes.Func)
				if fun == nil || node.Body == nil {
					return false
				}
				funcSig, _ := fun.Type().(*gotypes.Signature)
				inspectResourceReturns(node.Body, funcSig)
				return false
			case *ast.FuncLit:
				if node.Body == nil {
					return false
				}
				funcSig, _ := typeInfo.TypeOf(node).(*gotypes.Signature)
				inspectResourceReturns(node.Body, funcSig)
				return false
			case *ast.ReturnStmt:
				if sig == nil {
					return true
				}
				results := sig.Results()
				for i, resultExpr := range node.Results {
					if i >= results.Len() {
						break
					}
					if resultExpr == nil {
						continue
					}
					returnType := xgoutil.DerefType(results.At(i).Type())
					if !IsSpxResourceNameType(returnType) {
						continue
					}
					ast.Inspect(resultExpr, func(resultNode ast.Node) bool {
						if _, ok := resultNode.(*ast.FuncLit); ok {
							return false
						}
						literal, ok := resultNode.(*ast.BasicLit)
						if ok && literal.Kind == token.STRING {
							returnTypes[literal] = returnType
						}
						return true
					})
				}
			}
			return true
		})
	}

	for _, astFile := range astPkg.Files {
		inspectResourceReturns(astFile, nil)
	}
	return returnTypes
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

	switch fun := callExpr.Fun.(type) {
	case *ast.Ident:
		spxSpriteName := strings.TrimSuffix(path.Base(xgoutil.NodeFilename(result.proj.Fset, callExpr)), ".spx")
		return result.spxResourceSet.Sprite(spxSpriteName)
	case *ast.SelectorExpr:
		ident, ok := fun.X.(*ast.Ident)
		if !ok {
			return nil
		}
		obj := typeInfo.ObjectOf(ident)
		if obj == nil {
			return nil
		}
		if !result.hasSpxSpriteResourceAutoBinding(obj) {
			return nil
		}

		spxSpriteName := obj.Name()
		return result.spxResourceSet.Sprite(spxSpriteName)
	default:
		return nil
	}
}

// inspectSpxResourceRefForTypeAtExpr inspects an spx resource reference for a
// given type at an expression.
func (s *Server) inspectSpxResourceRefForTypeAtExpr(result *compileResult, expr ast.Expr, typ gotypes.Type, getSpriteContext func() *SpxSpriteResource) {
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return
	}
	exprTV := typeInfo.Types[expr]

	spxResourceName, ok := xgoutil.StringLitOrConstValue(expr, exprTV)
	if !ok {
		return
	}
	spxResourceRefKind := SpxResourceRefKindStringLiteral
	if _, ok := expr.(*ast.Ident); ok {
		spxResourceRefKind = SpxResourceRefKindConstantReference
	}

	switch canonicalSpxResourceNameType(typ) {
	case GetSpxBackdropNameType():
		const resourceType = "backdrop"

		if spxResourceName == "" {
			s.addEmptySpxResourceNameDiagnostic(result, expr, resourceType)
		} else {
			result.addSpxResourceRef(SpxResourceRef{
				ID:   SpxBackdropResourceID{BackdropName: spxResourceName},
				Kind: spxResourceRefKind,
				Node: expr,
			})
			if result.spxResourceSet.Backdrop(spxResourceName) == nil {
				s.addSpxResourceNotFoundDiagnostic(result, expr, resourceType, spxResourceName, "")
			}
		}
	case GetSpxSpriteNameType():
		const resourceType = "sprite"

		if spxResourceName == "" {
			s.addEmptySpxResourceNameDiagnostic(result, expr, resourceType)
		} else {
			result.addSpxResourceRef(SpxResourceRef{
				ID:   SpxSpriteResourceID{SpriteName: spxResourceName},
				Kind: spxResourceRefKind,
				Node: expr,
			})
			if result.spxResourceSet.Sprite(spxResourceName) == nil {
				s.addSpxResourceNotFoundDiagnostic(result, expr, resourceType, spxResourceName, "")
			}
		}
	case GetSpxSpriteCostumeNameType():
		spriteContext := getSpriteContext()
		if spriteContext == nil {
			break
		}

		if spxResourceName == "" {
			s.addEmptySpxResourceNameDiagnostic(result, expr, "sprite costume")
		} else {
			result.addSpxResourceRef(SpxResourceRef{
				ID:   SpxSpriteCostumeResourceID{SpriteName: spriteContext.Name, CostumeName: spxResourceName},
				Kind: spxResourceRefKind,
				Node: expr,
			})
			if spriteContext.Costume(spxResourceName) == nil {
				s.addSpxResourceNotFoundDiagnostic(result, expr, "costume", spxResourceName, spriteContext.Name)
			}
		}
	case GetSpxSpriteAnimationNameType():
		spriteContext := getSpriteContext()
		if spriteContext == nil {
			break
		}

		if spxResourceName == "" {
			s.addEmptySpxResourceNameDiagnostic(result, expr, "sprite animation")
		} else {
			result.addSpxResourceRef(SpxResourceRef{
				ID:   SpxSpriteAnimationResourceID{SpriteName: spriteContext.Name, AnimationName: spxResourceName},
				Kind: spxResourceRefKind,
				Node: expr,
			})
			if spriteContext.Animation(spxResourceName) == nil {
				s.addSpxResourceNotFoundDiagnostic(result, expr, "animation", spxResourceName, spriteContext.Name)
			}
		}
	case GetSpxSoundNameType():
		const resourceType = "sound"

		if spxResourceName == "" {
			s.addEmptySpxResourceNameDiagnostic(result, expr, resourceType)
		} else {
			result.addSpxResourceRef(SpxResourceRef{
				ID:   SpxSoundResourceID{SoundName: spxResourceName},
				Kind: spxResourceRefKind,
				Node: expr,
			})
			if result.spxResourceSet.Sound(spxResourceName) == nil {
				s.addSpxResourceNotFoundDiagnostic(result, expr, resourceType, spxResourceName, "")
			}
		}
	case GetSpxWidgetNameType():
		const resourceType = "widget"

		if spxResourceName == "" {
			s.addEmptySpxResourceNameDiagnostic(result, expr, resourceType)
		} else {
			result.addSpxResourceRef(SpxResourceRef{
				ID:   SpxWidgetResourceID{WidgetName: spxResourceName},
				Kind: spxResourceRefKind,
				Node: expr,
			})
			if result.spxResourceSet.Widget(spxResourceName) == nil {
				s.addSpxResourceNotFoundDiagnostic(result, expr, resourceType, spxResourceName, "")
			}
		}
	}
}

// addEmptySpxResourceNameDiagnostic adds a diagnostic for empty spx resource name.
func (s *Server) addEmptySpxResourceNameDiagnostic(result *compileResult, expr ast.Expr, resourceType string) {
	result.addDiagnostics(s.nodeDocumentURI(result.proj, expr), Diagnostic{
		Severity: SeverityError,
		Range:    RangeForNode(result.proj, expr),
		Message:  s.translate(fmt.Sprintf("%s resource name cannot be empty", resourceType)),
	})
}

// addSpxResourceNotFoundDiagnostic adds a diagnostic for spx resource not found.
func (s *Server) addSpxResourceNotFoundDiagnostic(result *compileResult, expr ast.Expr, resourceType, resourceName, contextSpriteName string) {
	message := fmt.Sprintf("%s resource %q not found", resourceType, resourceName)
	if contextSpriteName != "" {
		message = fmt.Sprintf("%s in sprite %q", message, contextSpriteName)
	}
	result.addDiagnostics(s.nodeDocumentURI(result.proj, expr), Diagnostic{
		Severity: SeverityError,
		Range:    RangeForNode(result.proj, expr),
		Message:  s.translate(message),
	})
}
