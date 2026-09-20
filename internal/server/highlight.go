package server

import (
	"fmt"
	"slices"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_documentHighlight
func (s *Server) textDocumentDocumentHighlight(params *DocumentHighlightParams) (*[]DocumentHighlight, error) {
	filename, err := s.fromDocumentURI(params.TextDocument.URI)
	if err != nil {
		return nil, fmt.Errorf("failed to get file path from document URI %q: %w", params.TextDocument.URI, err)
	}
	proj := s.requestProject()
	astPkg, _ := proj.ASTPackage()
	if astPkg == nil {
		return nil, nil
	}
	astFile := astPkg.Files[filename]
	if astFile == nil || !astFile.Pos().IsValid() {
		return nil, nil
	}
	position := ToPosition(proj, astFile, params.Position)
	typeInfo, _ := proj.TypeInfo()
	if typeInfo == nil {
		return nil, nil
	}
	_, targetObj, _ := sourceObjectAtPosition(proj, typeInfo, astFile, position)
	if targetObj == nil {
		return nil, nil
	}
	targetObj = typeInfo.ObjectDeclaration(targetObj)
	file := xgoutil.NodeTokenFile(proj.Fset, astFile)

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
		if !xgoutil.IsSourceIdent(file, astFile.Code, ident) {
			return false
		}
		obj := typeInfo.SourceObjectOf(ident)
		if typeInfo.ObjectDeclaration(obj) != targetObj {
			return true
		}
		path, _ := xgoutil.PathEnclosingInterval(astFile, ident.Pos(), ident.End())
		if len(path) < 2 {
			return true
		}

		kind := Text

		for _, parent := range path[1:] {
			switch p := parent.(type) {
			case *ast.KwargExpr:
				if p.Name == ident {
					kind = Read
				}
			case *ast.ValueSpec:
				if slices.Contains(p.Names, ident) {
					kind = Write
				} else if slices.Contains(p.Values, ast.Expr(ident)) {
					kind = Read
				}
			case *ast.Field:
				if slices.Contains(p.Names, ident) {
					kind = Write
				}
			case *ast.FuncDecl:
				if p.Name == ident {
					kind = Write
				}
			case *ast.OverloadFuncDecl:
				if p.Name == ident {
					kind = Write
				} else {
					kind = Read
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
				if slices.Contains(p.Lhs, ast.Expr(ident)) {
					kind = Write
				} else if slices.Contains(p.Rhs, ast.Expr(ident)) {
					kind = Read
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
			case *ast.ForPhrase:
				if p.Key == ident || p.Value == ident {
					kind = Write
				} else if p.X == ident || p.Cond == ident {
					kind = Read
				}
			case *ast.BinaryExpr,
				*ast.UnaryExpr,
				*ast.CallExpr,
				*ast.FuncDecorator,
				*ast.CompositeLit,
				*ast.IndexExpr,
				*ast.RangeExpr,
				*ast.ComprehensionExpr,
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
