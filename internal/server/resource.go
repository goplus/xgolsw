package server

import (
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
)

// resourceID is a comparable framework resource identity.
type resourceID interface {
	Name() string
	URI() XGoResourceURI
	ContextURI() XGoResourceContextURI
}

// resourceRef is a source reference to a framework resource.
type resourceRef struct {
	ID   resourceID
	Kind XGoResourceRefKind
	Node ast.Node
}

// XGoResourceRefKind is the source form of a resource reference.
type XGoResourceRefKind string

const (
	XGoResourceRefKindStringLiteral        XGoResourceRefKind = "stringLiteral"
	XGoResourceRefKindAutoBindingReference XGoResourceRefKind = "autoBindingReference"
	XGoResourceRefKindConstantReference    XGoResourceRefKind = "constantReference"
)

// resourceSourceNode selects the literal inside a conversion for source-facing
// ranges. The reference retains its full expression for type and rename analysis.
func resourceSourceNode(proj *xgo.Project, node ast.Node) ast.Node {
	if call, ok := node.(*ast.CallExpr); ok {
		if info, _ := proj.TypeInfo(); info != nil {
			if literal, _ := resourceStringLiteral(call, info); literal != nil {
				return literal
			}
		}
	}
	return node
}

// resourceNodeEnd returns the source end of a resource reference or constant
// initializer, including carriage returns omitted from raw literals.
func resourceNodeEnd(fset *token.FileSet, astFile *ast.File, node ast.Node) token.Pos {
	for {
		switch n := node.(type) {
		case *ast.BinaryExpr:
			node = n.Y
		case *ast.BasicLit:
			return basicLitEnd(fset, astFile, n)
		default:
			return node.End()
		}
	}
}

// resourceRange returns the physical UTF-16 range of a resource reference or
// initializer in astFile, ignoring line directives. The caller must resolve
// astFile from the current project source at node.Pos().
func resourceRange(proj *xgo.Project, astFile *ast.File, node ast.Node) Range {
	node = resourceSourceNode(proj, node)
	file := proj.Fset.File(node.Pos())
	return Range{
		Start: FromPosition(proj, astFile, file.PositionFor(node.Pos(), false)),
		End:   FromPosition(proj, astFile, file.PositionFor(resourceNodeEnd(proj.Fset, astFile, node), false)),
	}
}

// resourceAnalysis contains references, availability, and untranslated diagnostics.
// A nil contains function means that resource metadata is unavailable.
type resourceAnalysis struct {
	diagnostics      []sourceDiagnostic
	resourceRefs     []resourceRef
	seenResourceRefs map[resourceRef]struct{}
	contains         func(resourceID) bool
}

// resourceRefAtPosition returns the smallest resource reference containing
// position and its source file, including the position immediately after its
// source text.
func (r *resourceAnalysis) resourceRefAtPosition(proj *xgo.Project, position token.Position) (*resourceRef, *ast.File) {
	var (
		bestRef      *resourceRef
		bestFile     *ast.File
		bestNodeSpan int
	)
	fset := proj.Fset
	for _, ref := range r.resourceRefs {
		node := resourceSourceNode(proj, ref.Node)
		nodePos := fset.PositionFor(node.Pos(), false)
		if nodePos.Filename != position.Filename {
			continue
		}
		astFile := sourceASTFile(proj, node.Pos())
		if astFile == nil {
			continue
		}
		nodeEnd := fset.PositionFor(resourceNodeEnd(fset, astFile, node), false)
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

// addResourceRef adds a resolved reference, preserving the first occurrence.
func (r *resourceAnalysis) addResourceRef(ref resourceRef) {
	if r.seenResourceRefs == nil {
		r.seenResourceRefs = make(map[resourceRef]struct{})
	}

	if _, ok := r.seenResourceRefs[ref]; ok {
		return
	}
	r.seenResourceRefs[ref] = struct{}{}

	r.resourceRefs = append(r.resourceRefs, ref)
}
