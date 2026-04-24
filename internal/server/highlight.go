package server

import (
	"slices"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_documentHighlight
func (s *Server) textDocumentDocumentHighlight(params *DocumentHighlightParams) (*[]DocumentHighlight, error) {
	result, _, astFile, err := s.compileAndGetASTFileForDocumentURI(params.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	if astFile == nil {
		return nil, nil
	}
	position := ToPosition(result.proj, astFile, params.Position)
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil, nil
	}
	targetIdent := xgoutil.IdentAtPosition(result.proj.Fset, typeInfo, astFile, position)

	targetObj := typeInfo.ObjectOf(targetIdent)
	if targetObj == nil {
		return nil, nil
	}

	var highlights []DocumentHighlight
	ast.Inspect(astFile, func(node ast.Node) bool {
		if node == nil {
			return true
		}
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
			case *ast.ValueSpec:
				for _, name := range p.Names {
					if name == ident {
						kind = Write
						break
					}
				}
			case *ast.Field:
				if p.Names != nil {
					for _, name := range p.Names {
						if name == ident {
							kind = Write
							break
						}
					}
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
					for _, lhs := range p.Lhs {
						if lhs == ident {
							kind = Write
							break
						}
					}
					if kind != Write {
						for _, rhs := range p.Rhs {
							if rhs == ident {
								kind = Read
								break
							}
						}
					}
				case token.DEFINE:
					for _, lhs := range p.Lhs {
						if lhs == ident {
							kind = Write
							break
						}
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
				if p.Assign != nil {
					if assign, ok := p.Assign.(*ast.AssignStmt); ok {
						for _, lhs := range assign.Lhs {
							if lhs == ident {
								kind = Write
								break
							}
						}
					}
				}
			case *ast.BinaryExpr,
				*ast.UnaryExpr,
				*ast.CallExpr,
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

		highlights = append(highlights, DocumentHighlight{
			Range: RangeForNode(result.proj, ident),
			Kind:  kind,
		})
		return true
	})
	return &highlights, nil
}
