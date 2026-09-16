package server

import (
	"fmt"
	gotypes "go/types"
	"sync"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/analysis/ast/astutil"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// compileResult contains project type information and spx resource analysis.
type compileResult struct {
	definitionContext
	diagnosticResult

	// mainSpxFile is the registered spx project classfile path, or empty if
	// no project entry file is available.
	mainSpxFile string

	// spxSpriteTypes stores the spx sprite types.
	spxSpriteTypes map[gotypes.Type]struct{}

	// spxResourceSet is the set of spx resources.
	spxResourceSet SpxResourceSet

	// spxResourceSetErr distinguishes unavailable metadata from an empty set.
	spxResourceSetErr error

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

	for node := range xgoutil.PathEnclosingIntervalNodes(astFile, pos-1, pos, false) {
		call := callExprFromNode(node)
		if call == nil || !r.isSpxEventHandler(xgoutil.FuncFromCallExpr(typeInfo, call)) {
			continue
		}
		for expr := range callArgValueTypes(typeInfo, call) {
			var body *ast.BlockStmt
			switch expr := astutil.Unparen(expr).(type) {
			case *ast.FuncLit:
				body = expr.Body
			case *ast.LambdaExpr:
				body = expr.Body
			case *ast.ArrowExpr:
				if expr.Rarrow < pos && pos <= expr.End() {
					return true
				}
			}
			if body != nil && body.Pos() < pos && pos <= body.End() {
				return true
			}
		}
	}
	return false
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

// compile analyzes spx resources in the current project.
func (s *Server) compile() (*compileResult, error) {
	return s.compileAt(s.getProjWithFile())
}

// compileAt analyzes spx resources throughout the project. It returns nil when
// the project has no spx classfiles or its SDK is unavailable. Syntax errors,
// type errors, and analyzers are handled separately by diagnosticsAt.
func (s *Server) compileAt(snapshot *xgo.Project) (*compileResult, error) {
	var hasSpxFile bool
	for file := range snapshot.Files() {
		if spxClassForFile(snapshot, file) != nil {
			hasSpxFile = true
			break
		}
	}
	if !hasSpxFile {
		return nil, nil
	}

	result := newCompileResult(snapshot, s.lookupPkgDoc)
	if result.spxSymbols().pkg == nil {
		return nil, nil
	}
	astPkg, err := snapshot.ASTPackage()
	if astPkg == nil {
		return nil, err
	}
	for filename, astFile := range astPkg.Files {
		if spxClassForFile(snapshot, filename) == nil {
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

		if astFile.IsProj {
			result.mainSpxFile = filename
		}
	}
	typeInfo, _ := snapshot.TypeInfo()
	result.enumInfo = newEnumInfo(astPkg, typeInfo)
	for filename := range astPkg.Files {
		if named := spxSpriteTypeForFile(snapshot, filename); named != nil {
			result.spxSpriteTypes[named] = struct{}{}
		}
	}

	s.inspectForSpxResourceSet(snapshot, result)
	s.inspectForSpxResourceRefs(result)

	return result, nil
}

// inspectForSpxResourceSet loads the project's spx resource set.
func (s *Server) inspectForSpxResourceSet(snapshot *xgo.Project, result *compileResult) {
	spxResourceSet, err := NewSpxResourceSet(snapshot)
	result.spxResourceSetErr = err
	if err != nil {
		filename := result.mainSpxFile
		if filename == "" {
			filename = spxResourceRootDir + "/index.json"
		}
		documentURI := s.toDocumentURI(filename)
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
			switch result.spxResourceNameType(xgoutil.DerefType(tv.Type)) {
			case "BackdropName", "SpriteName", "SoundName", "WidgetName":
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

	file, _ := result.proj.ASTFile(result.mainSpxFile)
	gameType := classTypeForFile(result.proj, file)
	if gameType == nil {
		return
	}

	for structMember := range xgoutil.StructMembers(gameType, nil) {
		field, ok := structMember.Member.(*gotypes.Var)
		if !ok {
			continue
		}
		fieldType := resolvedNamedType(field.Type())
		if fieldType == nil {
			continue
		}
		if result.spxTypeName(fieldType) == "Sprite" || result.hasSpxSpriteType(fieldType) {
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
	switch result.spxTypeName(xgoutil.DerefType(funcSigRecv.Type())) {
	case "Sprite", "SpriteImpl":
		return spxSpriteResourceForCall(result, callExpr)
	}
	return nil
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

	resourceType := result.spxResourceNameType(typ)
	if resourceType == "" {
		return
	}
	resourceExprs[expr] = struct{}{}

	var id SpxResourceID
	switch resourceType {
	case "BackdropName":
		id = SpxBackdropResourceID{BackdropName: name}
	case "SpriteName":
		id = SpxSpriteResourceID{SpriteName: name}
	case "SpriteCostumeName":
		sprite := getSpriteContext()
		if sprite == nil {
			return
		}
		id = SpxSpriteCostumeResourceID{SpriteName: sprite.Name, CostumeName: name}
	case "SpriteAnimationName":
		sprite := getSpriteContext()
		if sprite == nil {
			return
		}
		id = SpxSpriteAnimationResourceID{SpriteName: sprite.Name, AnimationName: name}
	case "SoundName":
		id = SpxSoundResourceID{SoundName: name}
	case "WidgetName":
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
	if result.spxResourceSetErr == nil && !result.spxResourceSet.Contains(ref.ID) {
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
