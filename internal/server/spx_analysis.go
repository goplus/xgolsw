package server

import (
	"fmt"
	gotypes "go/types"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// spxAnalysis contains SDK symbols, sprite types, and resource analysis.
type spxAnalysis struct {
	*spxSymbols
	*resourceAnalysis

	// mainSpxFile is the registered spx project classfile path, or empty if
	// no project entry file is available.
	mainSpxFile string

	// spxSpriteTypes stores the spx sprite types.
	spxSpriteTypes map[gotypes.Type]struct{}

	// spxResourceSet is the set of spx resources.
	spxResourceSet SpxResourceSet

	// spxResourceSetErr distinguishes unavailable metadata from an empty set.
	spxResourceSetErr error

	// resourceLiterals retains resolved contexts for completion, including
	// empty names that cannot produce resource references.
	resourceLiterals map[*ast.BasicLit]resourceValue

	// spxSpriteResourceAutoBindings stores spx sprite resource auto-bindings.
	spxSpriteResourceAutoBindings map[gotypes.Object]struct{}
}

// newSpxAnalysis creates a new [spxAnalysis].
func newSpxAnalysis(proj *xgo.Project) *spxAnalysis {
	return &spxAnalysis{
		spxSymbols:                    newSpxSymbols(proj),
		resourceAnalysis:              &resourceAnalysis{proj: proj, diagnosticResult: newDiagnosticResult()},
		spxSpriteTypes:                make(map[gotypes.Type]struct{}),
		resourceLiterals:              make(map[*ast.BasicLit]resourceValue),
		spxSpriteResourceAutoBindings: make(map[gotypes.Object]struct{}),
	}
}

// hasSpxSpriteType reports whether the given type is an spx sprite type.
func (r *spxAnalysis) hasSpxSpriteType(typ gotypes.Type) bool {
	_, ok := r.spxSpriteTypes[typ]
	return ok
}

// hasSpxSpriteResourceAutoBinding reports whether the given object is an spx
// resource auto-binding.
func (r *spxAnalysis) hasSpxSpriteResourceAutoBinding(obj gotypes.Object) bool {
	_, ok := r.spxSpriteResourceAutoBindings[obj]
	return ok
}

// analyzeSpx analyzes spx resources throughout the project. It returns nil when
// the project has no spx classfiles or its SDK is unavailable. Syntax errors,
// type errors, and analyzers are handled separately by diagnosticsAt.
func (s *Server) analyzeSpx(snapshot *xgo.Project) (*spxAnalysis, error) {
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

	result := newSpxAnalysis(snapshot)
	if result.spxSymbols.pkg == nil {
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
	snapshot.TypeInfo()
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
func (s *Server) inspectForSpxResourceSet(snapshot *xgo.Project, result *spxAnalysis) {
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
	result.contains = result.spxResourceSet.Contains
}

// inspectForSpxResourceRefs collects source references using SDK resource types.
func (s *Server) inspectForSpxResourceRefs(result *spxAnalysis) {
	s.inspectForAutoBindingSpxResources(result)
	info, _ := result.proj.TypeInfo()
	callSprites := make(map[*ast.CallExpr]*SpxSpriteResource)
	for ref := range resourceReferences(result.proj, func(value resourceValue) (resourceID, bool) {
		if value.Intrinsic {
			switch result.spxResourceNameType(value.Type) {
			case "BackdropName", "SpriteName", "SoundName", "WidgetName":
			default:
				return nil, false
			}
		}
		id, recognized := result.resolveResourceID(value.Type, value.Name, func() *SpxSpriteResource {
			if value.Call == nil {
				return spxSpriteResourceForFile(result, result.proj.Fset.PositionFor(value.Expr.Pos(), false).Filename)
			}
			sprite, ok := callSprites[value.Call]
			if !ok {
				sprite = s.resolveSpxSpriteContextFromCallExpr(result, value.Call)
				callSprites[value.Call] = sprite
			}
			return sprite
		})
		if id != nil {
			if literal, _ := resourceStringLiteral(value.Expr, info); literal != nil {
				result.resourceLiterals[literal] = value
			}
		}
		return id, recognized
	}) {
		s.inspectSpxResourceRef(result, ref)
	}
}

// inspectForAutoBindingSpxResources inspects for auto-binding spx resources and
// their references.
func (s *Server) inspectForAutoBindingSpxResources(result *spxAnalysis) {
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
			result.addResourceRef(resourceRef{
				ID:   SpxSpriteResourceID{SpriteName: obj.Name()},
				Kind: XGoResourceRefKindAutoBindingReference,
				Node: ident,
			})
		}
	}
}

// resolveSpxSpriteContextFromCallExpr resolves the sprite context from a call expression.
func (s *Server) resolveSpxSpriteContextFromCallExpr(result *spxAnalysis, callExpr *ast.CallExpr) *SpxSpriteResource {
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

// resolveResourceID maps a registered SDK type and value to a resource identity.
// A recognized type with unavailable sprite context has no identity.
func (result *spxAnalysis) resolveResourceID(typ gotypes.Type, name string, getSpriteContext func() *SpxSpriteResource) (resourceID, bool) {
	resourceType := result.spxResourceNameType(typ)
	if resourceType == "" {
		return nil, false
	}
	var id resourceID
	switch resourceType {
	case "BackdropName":
		id = SpxBackdropResourceID{BackdropName: name}
	case "SpriteName":
		id = SpxSpriteResourceID{SpriteName: name}
	case "SpriteCostumeName":
		sprite := getSpriteContext()
		if sprite == nil {
			return nil, true
		}
		id = SpxSpriteCostumeResourceID{SpriteName: sprite.Name, CostumeName: name}
	case "SpriteAnimationName":
		sprite := getSpriteContext()
		if sprite == nil {
			return nil, true
		}
		id = SpxSpriteAnimationResourceID{SpriteName: sprite.Name, AnimationName: name}
	case "SoundName":
		id = SpxSoundResourceID{SoundName: name}
	case "WidgetName":
		id = SpxWidgetResourceID{WidgetName: name}
	}
	return id, true
}

// inspectSpxResourceRef records a resolved string resource reference and
// diagnoses empty names or missing resources without consulting SDK types.
func (s *Server) inspectSpxResourceRef(result *spxAnalysis, ref resourceRef) {
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
	result.addResourceRef(ref)
	if result.spxResourceSetErr == nil && !result.spxResourceSet.Contains(ref.ID) {
		s.addSpxResourceNotFoundDiagnostic(result, ref.Node, resourceType, ref.ID.Name(), spriteName)
	}
}

// addEmptySpxResourceNameDiagnostic diagnoses an empty SDK resource name.
func (s *Server) addEmptySpxResourceNameDiagnostic(result *spxAnalysis, expr ast.Node, resourceType string) {
	s.addResourceDiagnostic(result.resourceAnalysis, expr, fmt.Sprintf("%s resource name cannot be empty", resourceType))
}

// addSpxResourceNotFoundDiagnostic diagnoses a missing SDK resource.
func (s *Server) addSpxResourceNotFoundDiagnostic(result *spxAnalysis, expr ast.Node, resourceType, resourceName, contextSpriteName string) {
	message := fmt.Sprintf("%s resource %q not found", resourceType, resourceName)
	if contextSpriteName != "" {
		message = fmt.Sprintf("%s in sprite %q", message, contextSpriteName)
	}
	s.addResourceDiagnostic(result.resourceAnalysis, expr, message)
}
