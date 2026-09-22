package server

import (
	"fmt"
	"go/constant"
	gotypes "go/types"
	"slices"
	"strconv"
	"strings"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/analysis/ast/astutil"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
)

// resourceRenamePlan tracks constant values and source references for an entire
// rename batch. Initializer edits take precedence over edits inside them.
type resourceRenamePlan struct {
	initializers map[gotypes.Object]ast.Expr
	owners       map[ast.Node]ast.Expr
	refs         map[ast.Node]resourceRef
	values       map[ast.Expr]string
}

// newResourceRenamePlan identifies initializers that directly name renamed
// resources or whose uses all require the same new value.
func newResourceRenamePlan(proj *xgo.Project, result *resourceAnalysis, info *types.Info, renames map[resourceID]string) *resourceRenamePlan {
	p := &resourceRenamePlan{
		initializers: make(map[gotypes.Object]ast.Expr),
		owners:       make(map[ast.Node]ast.Expr),
		refs:         make(map[ast.Node]resourceRef),
		values:       make(map[ast.Expr]string),
	}
	// XGo constants in one declaration can share a position. Resolve by both
	// declaration position and name, including declarations absent from Defs.
	type constantKey struct {
		pos  token.Pos
		name string
	}
	declarations := make(map[constantKey]ast.Expr)
	astPkg, _ := proj.ASTPackage()
	if astPkg != nil {
		for _, file := range astPkg.Files {
			ast.Inspect(file, func(node ast.Node) bool {
				decl, ok := node.(*ast.GenDecl)
				if !ok || decl.Tok != token.CONST {
					return true
				}
				var values []ast.Expr
				for _, entry := range decl.Specs {
					spec := entry.(*ast.ValueSpec)
					if len(spec.Values) != 0 {
						values = spec.Values
					}
					for i := range min(len(spec.Names), len(values)) {
						expr := astutil.Unparen(values[i])
						declarations[constantKey{spec.Pos(), spec.Names[i].Name}] = expr
						ast.Inspect(expr, func(node ast.Node) bool {
							if node != nil {
								p.owners[node] = expr
							}
							return true
						})
					}
				}
				return false
			})
		}
	}
	for _, ref := range result.resourceRefs {
		p.refs[ref.Node] = ref
		if expr, ok := ref.Node.(ast.Expr); ok {
			p.refs[resourceStringOperand(expr, info)] = ref
		}
	}
	shared := make(map[ast.Expr]bool)
	for ident, obj := range info.Uses {
		if c, ok := obj.(*gotypes.Const); !ok || c.Pkg() != info.Pkg || c.Val().Kind() != constant.String {
			continue
		}
		expr := declarations[constantKey{obj.Pos(), obj.Name()}]
		if expr == nil {
			continue
		}
		p.initializers[obj] = expr
		ref, isResource := p.refs[ident]
		name, renamed := renames[ref.ID]
		if !isResource || !renamed || p.owners[ident] != nil {
			shared[expr] = true
			continue
		}
		if previous, ok := p.values[expr]; ok && previous != name {
			shared[expr] = true
		}
		p.values[expr] = name
	}
	for _, ref := range result.resourceRefs {
		if owner := p.owners[ref.Node]; owner == ref.Node {
			if _, renamed := renames[ref.ID]; !renamed {
				shared[owner] = true
			}
		}
	}
	for expr := range shared {
		delete(p.values, expr)
	}
	// A typed initializer is itself a resource reference. Update it even when
	// shared, then preserve other uses with their own required values.
	for _, ref := range result.resourceRefs {
		if owner := p.owners[ref.Node]; owner == ref.Node {
			if name, renamed := renames[ref.ID]; renamed {
				p.values[owner] = name
			}
		}
	}
	return p
}

// renameResourcesAtRefs builds non-overlapping edits for all requested resource
// renames while preserving constant uses that require different values.
func (s *Server) renameResourcesAtRefs(proj *xgo.Project, result *resourceAnalysis, renames map[resourceID]string) (map[DocumentURI][]TextEdit, error) {
	changes := make(map[DocumentURI][]TextEdit)
	info, _ := proj.TypeInfo()
	if info == nil || len(renames) == 0 {
		return changes, nil
	}
	plan := newResourceRenamePlan(proj, result, info, renames)
	seen := make(map[DocumentURI]map[TextEdit]bool)
	covered := make(map[*ast.Ident]bool)
	addEdit := func(node ast.Node, name string, quoted bool) error {
		var intrinsic bool
		if expr, ok := node.(ast.Expr); ok && quoted {
			intrinsic = result.expressions[expr].value.Intrinsic
			node = resourceStringOperand(expr, info)
		}
		file := sourceASTFile(proj, node.Pos())
		if file == nil {
			return nil
		}
		tokenFile := proj.Fset.File(node.Pos())
		edit := TextEdit{Range: Range{
			Start: FromPosition(proj, file, tokenFile.PositionFor(node.Pos(), false)),
			End:   FromPosition(proj, file, tokenFile.PositionFor(resourceNodeEnd(proj.Fset, file, node), false)),
		}, NewText: name}
		if quoted {
			edit.NewText = strings.ReplaceAll(strconv.Quote(name), "$", `\x24`)
			if lit, ok := node.(*ast.BasicLit); ok {
				raw := lit.Value[0] == '`'
				if !raw || strconv.CanBackquote(name) && !strings.ContainsRune(name, '$') {
					edit.Range.Start.Character++
					edit.Range.End.Character--
					if raw {
						edit.NewText = name
					} else {
						edit.NewText = edit.NewText[1 : len(edit.NewText)-1]
					}
				}
			} else if expr, ok := node.(ast.Expr); ok {
				// Defined types preserve language semantics. Intrinsic aliases
				// also preserve the resource collection on subsequent requests.
				if typ := resourceExpressionType(expr, info); typ != nil {
					_, named := gotypes.Unalias(typ).(*gotypes.Named)
					_, alias := typ.(*gotypes.Alias)
					if named || alias && intrinsic {
						display := newTypeDisplay(proj, file, node.Pos())
						name, ok := display.sourceTypeString(typ)
						if !ok {
							return fmt.Errorf("cannot preserve resource constant type %q at %s", display.typeString(typ), proj.Fset.PositionFor(node.Pos(), false))
						}
						edit.NewText = name + "(" + edit.NewText + ")"
					}
				}
			}
		}
		ast.Inspect(node, func(node ast.Node) bool {
			if ident, ok := node.(*ast.Ident); ok {
				covered[ident] = true
			}
			return true
		})
		uri := s.toDocumentURI(tokenFile.Name())
		if seen[uri] == nil {
			seen[uri] = make(map[TextEdit]bool)
		}
		if !seen[uri][edit] {
			seen[uri][edit] = true
			changes[uri] = append(changes[uri], edit)
		}
		return nil
	}
	// Preserve a dependent initializer as a complete value. Replacing its
	// operands independently can change its type or introduce references to
	// the old resource through newly inserted type conversions.
	preserved := make(map[ast.Expr]bool)
	for ident, obj := range info.Uses {
		value, changed := plan.values[plan.initializers[obj]]
		owner := plan.owners[ident]
		if !changed || owner == nil || preserved[owner] || constant.StringVal(obj.(*gotypes.Const).Val()) == value {
			continue
		}
		if _, replaced := plan.values[owner]; replaced {
			continue
		}
		text, err := resourceConstantSource(proj, owner, info)
		if err != nil {
			return nil, err
		}
		if err := addEdit(owner, text, false); err != nil {
			return nil, err
		}
		preserved[owner] = true
	}
	for expr, name := range plan.values {
		if obj := resourceConstantObject(expr, info); obj != nil {
			initializer := plan.initializers[obj]
			if value, changed := plan.values[initializer]; changed && value == name {
				continue
			}
		}
		if err := addEdit(expr, name, true); err != nil {
			return nil, err
		}
	}
	for _, ref := range result.resourceRefs {
		name, renamed := renames[ref.ID]
		if owner := plan.owners[ref.Node]; owner != nil {
			if preserved[owner] || !renamed || owner == ref.Node {
				continue
			}
			if _, replaced := plan.values[owner]; replaced {
				continue
			}
			switch ref.Kind {
			case XGoResourceRefKindStringLiteral, XGoResourceRefKindStringExpression:
				return nil, fmt.Errorf("cannot rename a resource within a derived constant at %s", proj.Fset.PositionFor(ref.Node.Pos(), false))
			}
			continue
		}
		if ref.Kind != XGoResourceRefKindConstantReference {
			if !renamed {
				continue
			}
			if err := addEdit(ref.Node, name, ref.Kind != XGoResourceRefKindAutoBindingReference); err != nil {
				return nil, err
			}
			continue
		}
		obj := resourceConstantObject(ref.Node, info)
		initializer := plan.initializers[obj]
		current := ref.ID.Name()
		if value, changed := plan.values[initializer]; changed {
			current = value
		}
		if !renamed {
			name = ref.ID.Name()
		}
		if current == name {
			continue
		}
		if err := addEdit(ref.Node, name, true); err != nil {
			return nil, err
		}
	}
	for ident, obj := range info.Uses {
		if covered[ident] {
			continue
		}
		value, changed := plan.values[plan.initializers[obj]]
		if !changed {
			continue
		}
		owner := plan.owners[ident]
		if _, replaced := plan.values[owner]; replaced {
			continue
		}
		if _, resource := plan.refs[ident]; resource && owner == nil {
			continue
		}
		// Preserve ordinary uses and dependencies of unchanged constants.
		original := constant.StringVal(obj.(*gotypes.Const).Val())
		if original == value {
			continue
		}
		if err := addEdit(ident, original, true); err != nil {
			return nil, err
		}
	}
	for _, edits := range changes {
		slices.SortFunc(edits, func(a, b TextEdit) int { return comparePositions(a.Range.Start, b.Range.Start) })
	}
	return changes, nil
}

// resourceConstantSource preserves the value and type of a dependent initializer.
// An iota expression cannot be folded because omitted initializers reevaluate it.
func resourceConstantSource(proj *xgo.Project, expr ast.Expr, info *types.Info) (string, error) {
	value := info.Types[expr].Value
	var usesIota bool
	ast.Inspect(expr, func(node ast.Node) bool {
		if ident, ok := node.(*ast.Ident); ok && info.ObjectOf(ident) == gotypes.Universe.Lookup("iota") {
			usesIota = true
		}
		return !usesIota
	})
	if value == nil || usesIota {
		return "", fmt.Errorf("cannot preserve a derived constant at %s", proj.Fset.PositionFor(expr.Pos(), false))
	}
	var text string
	switch value.Kind() {
	case constant.String:
		text = strings.ReplaceAll(strconv.Quote(constant.StringVal(value)), "$", `\x24`)
	case constant.Bool:
		// Boolean spellings are identifiers and may be shadowed in source.
		text = "(0 == 0)"
		if !constant.BoolVal(value) {
			text = "(0 != 0)"
		}
	case constant.Int:
		text = value.ExactString()
	default:
		return "", fmt.Errorf("cannot preserve a derived constant at %s", proj.Fset.PositionFor(expr.Pos(), false))
	}
	typ := resourceExpressionType(expr, info)
	if basic, ok := typ.(*gotypes.Basic); ok && basic.Info()&gotypes.IsUntyped != 0 {
		return text, nil
	}
	display := newTypeDisplay(proj, sourceASTFile(proj, expr.Pos()), expr.Pos())
	name, ok := display.sourceTypeString(typ)
	if !ok {
		return "", fmt.Errorf("cannot preserve resource constant type %q at %s", display.typeString(typ), proj.Fset.PositionFor(expr.Pos(), false))
	}
	return name + "(" + text + ")", nil
}

// resourceConstantObject returns the constant named by a reference, including
// imported selectors and identifiers wrapped in string conversions.
func resourceConstantObject(node ast.Node, info *types.Info) gotypes.Object {
	expr, ok := node.(ast.Expr)
	if !ok {
		return nil
	}
	switch expr := resourceStringOperand(expr, info).(type) {
	case *ast.Ident:
		return info.ObjectOf(expr)
	case *ast.SelectorExpr:
		return info.ObjectOf(expr.Sel)
	}
	return nil
}

// renameResources dispatches resource edits to the current framework.
func (s *Server) renameResources(params []XGoRenameResourceParams) (*WorkspaceEdit, error) {
	proj := s.requestProject()
	result, err := analyzeFramework(proj)
	if err != nil {
		return nil, err
	}
	if result == nil || result.renameResources == nil {
		return nil, fmt.Errorf("resource analysis is unavailable")
	}
	return result.renameResources(s, proj, params)
}
