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
func newResourceRenamePlan(result *resourceAnalysis, info *types.Info, renames map[resourceID]string) *resourceRenamePlan {
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
	astPkg, _ := result.proj.ASTPackage()
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
	}
	shared := make(map[ast.Expr]bool)
	for ident, obj := range info.Uses {
		if c, ok := obj.(*gotypes.Const); !ok || c.Val().Kind() != constant.String {
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
func (s *Server) renameResourcesAtRefs(result *resourceAnalysis, renames map[resourceID]string) (map[DocumentURI][]TextEdit, error) {
	changes := make(map[DocumentURI][]TextEdit)
	info, _ := result.proj.TypeInfo()
	if info == nil || len(renames) == 0 {
		return changes, nil
	}
	plan := newResourceRenamePlan(result, info, renames)
	seen := make(map[DocumentURI]map[TextEdit]bool)
	addEdit := func(node ast.Node, name string, quoted bool) error {
		if expr, ok := node.(ast.Expr); ok && quoted {
			if literal, _ := resourceStringLiteral(expr, info); literal != nil {
				node = literal
			}
		}
		file := sourceASTFile(result.proj, node.Pos())
		if file == nil {
			return nil
		}
		edit := TextEdit{Range: resourceRange(result.proj, file, node), NewText: name}
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
				// Retain defined string types when replacing a typed expression.
				if typ := info.TypeOf(expr); typ != nil {
					if _, ok := gotypes.Unalias(typ).(*gotypes.Named); ok {
						display := newTypeDisplay(result.proj, file, node.Pos())
						name, ok := display.sourceTypeString(typ)
						if !ok {
							return fmt.Errorf("cannot preserve resource constant type %q at %s", display.typeString(typ), result.proj.Fset.PositionFor(node.Pos(), false))
						}
						edit.NewText = name + "(" + edit.NewText + ")"
					}
				}
			}
		}
		uri := s.toDocumentURI(result.proj.Fset.File(node.Pos()).Name())
		if seen[uri] == nil {
			seen[uri] = make(map[TextEdit]bool)
		}
		if !seen[uri][edit] {
			seen[uri][edit] = true
			changes[uri] = append(changes[uri], edit)
		}
		return nil
	}
	for expr, name := range plan.values {
		if ident, ok := expr.(*ast.Ident); ok {
			initializer := plan.initializers[info.ObjectOf(ident)]
			if value, changed := plan.values[initializer]; changed && value == name {
				continue
			}
		}
		if err := addEdit(expr, name, true); err != nil {
			return nil, err
		}
	}
	for _, ref := range result.resourceRefs {
		if owner := plan.owners[ref.Node]; owner != nil {
			if _, renamed := renames[ref.ID]; renamed && ref.Kind == XGoResourceRefKindStringLiteral && owner != ref.Node {
				if _, replaced := plan.values[owner]; !replaced {
					return nil, fmt.Errorf("cannot rename a resource within a derived constant at %s", result.proj.Fset.PositionFor(ref.Node.Pos(), false))
				}
			}
			continue
		}
		name, renamed := renames[ref.ID]
		if ref.Kind != XGoResourceRefKindConstantReference {
			if !renamed {
				continue
			}
			if err := addEdit(ref.Node, name, ref.Kind == XGoResourceRefKindStringLiteral); err != nil {
				return nil, err
			}
			continue
		}
		obj := info.ObjectOf(ref.Node.(*ast.Ident))
		initializer := plan.initializers[obj]
		if initializer == nil {
			continue
		}
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
		// Retaining a defined string type here would introduce a resource
		// conversion with the old name inside an unchanged initializer.
		if _, named := gotypes.Unalias(obj.Type()).(*gotypes.Named); named && owner != nil {
			return nil, fmt.Errorf("cannot preserve a derived constant at %s", result.proj.Fset.PositionFor(owner.Pos(), false))
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

// renameResources dispatches resource edits to the current framework.
func (s *Server) renameResources(params []XGoRenameResourceParams) (*WorkspaceEdit, error) {
	result, err := s.analyzeFramework(s.getProjWithFile())
	if err != nil {
		return nil, err
	}
	if result == nil || result.renameResources == nil {
		return nil, fmt.Errorf("resource analysis is unavailable")
	}
	return result.renameResources(params)
}
