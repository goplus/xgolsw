package server

import (
	"fmt"
	"slices"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_documentHighlight
func (s *Server) textDocumentDocumentHighlight(params *DocumentHighlightParams) (*[]DocumentHighlight, error) {
	filename, err := s.fromDocumentURI(params.TextDocument.URI)
	if err != nil {
		return nil, fmt.Errorf("failed to get file path from document URI %q: %w", params.TextDocument.URI, err)
	}
	proj := s.getProjWithFile()
	astPkg, _ := proj.ASTPackage()
	if astPkg == nil {
		return nil, nil
	}
	astFile := astPkg.Files[filename]
	if astFile == nil {
		return nil, nil
	}
	position := ToPosition(proj, astFile, params.Position)
	typeInfo, _ := proj.TypeInfo()
	if typeInfo == nil {
		return nil, nil
	}
	_, targetObj, _ := objectAtPosition(proj, typeInfo, astFile, position)
	if targetObj == nil {
		return nil, nil
	}

	var highlights []DocumentHighlight
	appendHighlight := func(highlight DocumentHighlight) {
		if slices.Contains(highlights, highlight) {
			return
		}
		highlights = append(highlights, highlight)
	}
	ast.Inspect(astFile, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		obj := typeInfo.ObjectOf(ident)
		if obj != targetObj {
			return true
		}
		path, _ := xgoutil.PathEnclosingInterval(astFile, ident.Pos(), ident.End())
		if len(path) < 2 {
			return true
		}

		kind := Text

		for _, parent := range slices.Backward(path[:len(path)-1]) {
			switch p := parent.(type) {
			case *ast.KwargExpr:
				if p.Name == ident {
					kind = Read
				}
			case *ast.ValueSpec:
				if slices.Contains(p.Names, ident) {
					kind = Write
				}
			case *ast.Field:
				if slices.Contains(p.Names, ident) {
					kind = Write
				}
			case *ast.FuncDecl:
				if p.Name == ident {
					kind = Write
				}
			case *ast.TypeSpec:
				if p.Name == ident {
					kind = Write
				}
			case *ast.LabeledStmt:
				if p.Label == ident {
					kind = Write
				}
			case *ast.AssignStmt:
				switch p.Tok {
				case token.ASSIGN:
					if slices.Contains(p.Lhs, ast.Expr(ident)) {
						kind = Write
					} else if slices.Contains(p.Rhs, ast.Expr(ident)) {
						kind = Read
					}
				case token.DEFINE:
					if slices.Contains(p.Lhs, ast.Expr(ident)) {
						kind = Write
					}
				default:
					kind = Write
				}
			case *ast.IncDecStmt:
				if p.X == ident {
					kind = Write
				}
			case *ast.RangeStmt:
				if p.X == ident {
					kind = Read
				} else if p.Key == ident || p.Value == ident {
					kind = Write
				}
			case *ast.TypeSwitchStmt:
				if assign, ok := p.Assign.(*ast.AssignStmt); ok && slices.Contains(assign.Lhs, ast.Expr(ident)) {
					kind = Write
				}
			case *ast.BinaryExpr,
				*ast.UnaryExpr,
				*ast.CallExpr,
				*ast.FuncDecorator,
				*ast.CompositeLit,
				*ast.IndexExpr,
				*ast.ReturnStmt,
				*ast.SendStmt:
				kind = Read
			case *ast.KeyValueExpr:
				if p.Key == ident || p.Value == ident {
					kind = Read
				}
			case *ast.SelectorExpr:
				if p.X == ident {
					kind = Read
				}
			}
			if kind != Text {
				break
			}
		}

		appendHighlight(DocumentHighlight{
			Range: RangeForNode(proj, ident),
			Kind:  kind,
		})
		return true
	})
	for _, loc := range s.kwargReferenceLocations(proj, targetObj) {
		if loc.URI != params.TextDocument.URI {
			continue
		}
		appendHighlight(DocumentHighlight{
			Range: loc.Range,
			Kind:  Read,
		})
	}
	return &highlights, nil
}
