package server

import (
	"bytes"
	"fmt"
	"iter"
	"slices"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/format"
	"github.com/goplus/xgo/parser"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// classVarSource records byte offsets for a var declaration and its documentation.
// bodyEnd excludes trailing comments and separators, while end includes them.
type classVarSource struct {
	decl                *ast.GenDecl
	start, bodyEnd, end int
}

// classVarSources collects declaration byte ranges and indexes physical comment ends.
func classVarSources(proj *xgo.Project, file *ast.File, decls []*ast.GenDecl) ([]classVarSource, map[int]int) {
	code := file.Code
	tokenFile := xgoutil.NodeTokenFile(proj.Fset, file)
	commentEnds := make(map[int]int)
	for _, group := range file.Comments {
		for _, comment := range group.List {
			start := tokenFile.Offset(comment.Pos())
			commentEnds[start] = classVarTextEnd(code, start, comment.Text)
		}
	}
	sources := make([]classVarSource, 0, len(decls))
	for _, decl := range decls {
		start := tokenFile.Offset(decl.Pos())
		if decl.Doc != nil {
			start = tokenFile.Offset(decl.Doc.Pos())
		}
		bodyEnd := tokenFile.Offset(classVarDeclEnd(proj, file, decl))
		end := classVarSourceEnd(code, bodyEnd, commentEnds)
		sources = append(sources, classVarSource{decl: decl, start: start, bodyEnd: bodyEnd, end: end})
	}
	return sources, commentEnds
}

// formatClassVarDecls renders the supplied var declarations as one class field
// block in source order.
func formatClassVarDecls(proj *xgo.Project, filename string, file *ast.File, sources []classVarSource, commentEnds map[int]int) ([]byte, error) {
	code := file.Code
	tokenFile := xgoutil.NodeTokenFile(proj.Fset, file)
	var merged bytes.Buffer
	// Keep documentation outside only when the first declaration is grouped.
	// Other declaration documentation stays with its fields inside the block.
	first := sources[0]
	if first.decl.Lparen.IsValid() && first.decl.Doc != nil {
		merged.Write(code[first.start:tokenFile.Offset(first.decl.Pos())])
	}
	merged.WriteString("var (\n")
	for i, source := range sources {
		decl := source.decl
		if i > 0 {
			merged.WriteByte('\n')
		}
		if decl.Doc != nil && (i > 0 || !decl.Lparen.IsValid()) {
			merged.Write(code[source.start:tokenFile.Offset(decl.Pos())])
		}
		keywordEnd := tokenFile.Offset(decl.Pos()) + len("var")
		if decl.Lparen.IsValid() {
			merged.Write(code[keywordEnd:tokenFile.Offset(decl.Lparen)])
			merged.Write(code[tokenFile.Offset(decl.Lparen)+1 : tokenFile.Offset(decl.Rparen)])
			merged.WriteByte('\n')
		} else {
			merged.Write(code[keywordEnd:source.bodyEnd])
		}
		// Preserve trailing comments while dropping declaration separators.
		for cursor := source.bodyEnd; cursor < source.end; {
			if end, ok := commentEnds[cursor]; ok {
				merged.WriteByte(' ')
				merged.Write(code[cursor:end])
				cursor = end
			} else {
				cursor++
			}
		}
		merged.WriteByte('\n')
	}
	merged.WriteString(")\n")
	return formatClassSource(proj, filename, merged.Bytes())
}

// classVarDeclEnd returns a var declaration's physical source end, including
// field tags omitted by [ast.ValueSpec.End] and CR omitted from raw string values.
func classVarDeclEnd(proj *xgo.Project, file *ast.File, decl *ast.GenDecl) token.Pos {
	end := decl.End()
	tokenFile := xgoutil.NodeTokenFile(proj.Fset, file)
	ast.Inspect(decl, func(node ast.Node) bool {
		if lit, ok := node.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			offset := classVarTextEnd(file.Code, tokenFile.Offset(lit.Pos()), lit.Value)
			end = max(end, tokenFile.Pos(offset))
		}
		return true
	})
	return end
}

// classVarSourceEnd includes explicit separators and comments attached to the
// declaration's final line, without consuming the next declaration.
func classVarSourceEnd(code []byte, end int, commentEnds map[int]int) int {
	for cursor := end; cursor < len(code); {
		switch code[cursor] {
		case ' ', '\t':
			cursor++
		case ';':
			cursor++
			end = cursor
		default:
			commentEnd, ok := commentEnds[cursor]
			if !ok {
				return end
			}
			cursor = commentEnd
			end = cursor
		}
	}
	return end
}

// classVarEditOffsets extends a declaration's byte range over leading horizontal
// whitespace and a trailing line ending when no code follows on that line.
func classVarEditOffsets(code []byte, start, end int) (int, int) {
	for start > 0 && (code[start-1] == ' ' || code[start-1] == '\t') {
		start--
	}
	cursor := end
	for cursor < len(code) && (code[cursor] == ' ' || code[cursor] == '\t') {
		cursor++
	}
	if cursor < len(code) && code[cursor] == '\r' {
		cursor++
	}
	if cursor < len(code) && code[cursor] == '\n' {
		end = cursor + 1
	} else if cursor == len(code) {
		end = cursor
	}
	return start, end
}

// classVarTextEnd restores the physical end of scanner text, which omits CR
// characters in comments and raw string literals.
func classVarTextEnd(code []byte, start int, text string) int {
	end := start
	for i := 0; i < len(text); end++ {
		if code[end] != '\r' || text[i] == '\r' {
			i++
		}
	}
	return end
}

// formatClassSource parses and formats class source with its field tags intact.
func formatClassSource(proj *xgo.Project, filename string, source []byte) ([]byte, error) {
	file, err := parser.ParseEntry(proj.Fset, filename, source, parser.Config{
		ClassInfo: proj.Module().ClassInfo,
		Mode:      parser.ParseComments | parser.AllErrors,
	})
	if err != nil {
		return nil, err
	}
	return formatFileWithClassTags(proj, filename, file)
}

// formatFileWithClassTags formats an owned AST and sorts its imports in place.
// It restores class field tags in the output because XGo's printer omits them
// for a single spec or places them before initializers, contrary to the grammar.
func formatFileWithClassTags(proj *xgo.Project, filename string, file *ast.File) ([]byte, error) {
	specs := slices.Collect(classVarSpecs(file))
	tags := make([]*ast.BasicLit, len(specs))
	hasTags := false
	for i, spec := range specs {
		tags[i] = spec.Tag
		hasTags = hasTags || spec.Tag != nil
	}
	if hasTags {
		for _, spec := range specs {
			spec.Tag = nil
		}
		defer func() {
			for i, spec := range specs {
				spec.Tag = tags[i]
			}
		}()
	}
	// Sort first so format.Node does not reparse class syntax without its framework.
	ast.SortImports(proj.Fset, file)
	var output bytes.Buffer
	if err := format.Node(&output, proj.Fset, file); err != nil {
		return nil, err
	}
	if !hasTags {
		return output.Bytes(), nil
	}
	printed, err := parser.ParseEntry(proj.Fset, filename, output.Bytes(), parser.Config{
		ClassInfo: proj.Module().ClassInfo,
		Mode:      parser.ParseComments | parser.AllErrors,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to parse formatted class fields: %w", err)
	}
	printedSpecs := slices.Collect(classVarSpecs(printed))
	if len(printedSpecs) != len(specs) {
		return nil, fmt.Errorf("formatted class field count changed from %d to %d", len(specs), len(printedSpecs))
	}
	tokenFile := xgoutil.NodeTokenFile(proj.Fset, printed)
	var restored bytes.Buffer
	previous := 0
	for i, tag := range tags {
		if tag == nil {
			continue
		}
		end := tokenFile.Offset(printedSpecs[i].End())
		restored.Write(output.Bytes()[previous:end])
		restored.WriteByte(' ')
		restored.WriteString(tag.Value)
		previous = end
	}
	restored.Write(output.Bytes()[previous:])
	return restored.Bytes(), nil
}

// classVarSpecs yields direct top-level var specs in source order.
func classVarSpecs(file *ast.File) iter.Seq[*ast.ValueSpec] {
	return func(yield func(*ast.ValueSpec) bool) {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				if !yield(spec.(*ast.ValueSpec)) {
					return
				}
			}
		}
	}
}
