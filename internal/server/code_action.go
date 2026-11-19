package server

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/protocol"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// codeActionKindMergeVarDeclarations identifies the classfile var declaration merge fix.
const codeActionKindMergeVarDeclarations protocol.CodeActionKind = "quickfix.xgo.mergeVarDeclarations"

// textDocumentCodeAction returns explicit fixes for the requested source range.
// It parses a fresh snapshot because type checking can rewrite shared syntax.
func (s *Server) textDocumentCodeAction(params *CodeActionParams) ([]CodeAction, error) {
	capabilities, initialized := s.clientCapabilitiesAfterInitialize()
	if initialized && capabilities.TextDocument.CodeAction.CodeActionLiteralSupport.CodeActionKind.ValueSet == nil {
		return nil, nil
	}
	if len(params.Context.Only) > 0 {
		wantsMerge := false
		for _, kind := range params.Context.Only {
			if kind == protocol.Empty || kind == codeActionKindMergeVarDeclarations ||
				strings.HasPrefix(string(codeActionKindMergeVarDeclarations), string(kind)+".") {
				wantsMerge = true
				break
			}
		}
		if !wantsMerge {
			return nil, nil
		}
	}
	filename, err := s.fromDocumentURI(params.TextDocument.URI)
	if err != nil {
		return nil, fmt.Errorf("failed to get file path from document URI %q: %w", params.TextDocument.URI, err)
	}
	snapshot, version := s.codeActionSnapshot(filename)
	proj := snapshot.Fork()
	if !proj.IsSourceFile(filename) {
		return nil, nil
	}
	file, err := proj.ASTFile(filename)
	if err != nil || file == nil || !file.IsClass {
		return nil, nil
	}
	if xgoutil.IsFlatClassFragment(file, filename, proj.Module().LookupClass) {
		// Flat fragments cannot declare class fields, even in a single block.
		return nil, nil
	}
	action, err := mergeClassVarDeclsAction(proj, filename, params.TextDocument.URI, file, params.Range)
	if err != nil {
		return nil, err
	}
	if action == nil {
		return nil, nil
	}
	if workspaceEdit := capabilities.Workspace.WorkspaceEdit; workspaceEdit != nil && workspaceEdit.DocumentChanges && version != nil {
		edits := action.Edit.Changes[params.TextDocument.URI]
		documentEdits := make([]protocol.Or_TextDocumentEdit_edits_Elem, len(edits))
		for i, edit := range edits {
			documentEdits[i] = protocol.Or_TextDocumentEdit_edits_Elem{Value: edit}
		}
		action.Edit = &WorkspaceEdit{DocumentChanges: []protocol.DocumentChange{{
			TextDocumentEdit: &protocol.TextDocumentEdit{
				TextDocument: protocol.OptionalVersionedTextDocumentIdentifier{
					TextDocumentIdentifier: params.TextDocument,
					Version:                *version,
				},
				Edits: documentEdits,
			},
		}}}
	}
	return []CodeAction{*action}, nil
}

// codeActionSnapshot captures source and its open-document version under the
// same lock so a later document update cannot give old edits a newer version.
// The returned snapshot must be forked before parsing or modification.
func (s *Server) codeActionSnapshot(filename string) (*xgo.Project, *int32) {
	files := s.readProviderFiles()
	s.projectMu.Lock()
	defer s.projectMu.Unlock()
	s.syncFilesLocked(files)
	var version *int32
	if file := s.openFiles[filename]; file != nil {
		version = new(int32(file.Version))
	}
	return s.projectSnapshotLocked(), version
}

// mergeClassVarDeclsAction prepares a fix that merges top-level var declarations
// in source order without applying the edits to the project.
// Local declarations remain untouched, including those in the shadow entry.
func mergeClassVarDeclsAction(proj *xgo.Project, filename string, uri DocumentURI, file *ast.File, selection Range) (*CodeAction, error) {
	var decls []*ast.GenDecl
	var firstFunc ast.Decl
	for _, decl := range file.Decls {
		switch decl := decl.(type) {
		case *ast.GenDecl:
			if decl.Tok == token.VAR {
				decls = append(decls, decl)
			}
		case *ast.FuncDecl:
			if firstFunc == nil && !decl.Shadow {
				firstFunc = decl
			}
		case *ast.OverloadFuncDecl:
			if firstFunc == nil {
				firstFunc = decl
			}
		}
	}
	if len(decls) < 2 {
		return nil, nil
	}

	code := file.Code
	tokenFile := xgoutil.NodeTokenFile(proj.Fset, file)
	lineOffsets := proj.ASTFileLineOffsets(file)
	sources, commentEnds := classVarSources(proj, file, decls)
	var diagnostics []Diagnostic
	relevant := false
	for _, source := range sources {
		decl := source.decl
		span := Range{
			Start: positionForOffset(code, lineOffsets, source.start),
			End:   positionForOffset(code, lineOffsets, source.end),
		}
		if selection.Start == selection.End {
			relevant = relevant || comparePositions(span.Start, selection.Start) <= 0 &&
				comparePositions(selection.Start, span.End) <= 0
		} else {
			relevant = relevant || IsRangesOverlap(span, selection)
		}
		// Match compiler diagnostic ranges, which may exclude a field tag.
		if decl != file.ClassFields {
			message := "multiple top-level var declarations in classfile"
			if file.ClassFields == nil {
				message = "class fields declaration must be declared before functions"
			}
			diagnostics = append(diagnostics, Diagnostic{
				Range:    RangeForASTFileNode(proj, file, decl),
				Severity: SeverityError,
				Message:  message,
			})
		}
	}
	if !relevant {
		return nil, nil
	}

	formatted, err := formatClassVarDecls(proj, filename, file, sources, commentEnds)
	if err != nil {
		return nil, fmt.Errorf("failed to format merged var declaration: %w", err)
	}
	lineEnding := "\n"
	if bytes.Contains(code, []byte("\r\n")) {
		lineEnding = "\r\n"
		formatted = bytes.ReplaceAll(formatted, []byte("\n"), []byte("\r\n"))
	}

	// Keep fields before every function, including explicit receiver methods.
	first := sources[0]
	anchor := first.start
	if firstFunc != nil && firstFunc.Pos() < first.decl.Pos() {
		anchor = tokenFile.Offset(firstFunc.Pos())
		if doc := getDeclDoc(firstFunc); doc != nil {
			anchor = tokenFile.Offset(doc.Pos())
		}
	}
	edits := make([]TextEdit, 0, len(sources)+1)
	for i, source := range sources {
		start, end := classVarEditOffsets(code, source.start, source.end)
		var replacement string
		if i == 0 && anchor == first.start {
			replacement = string(formatted)
		} else {
			// Remove empty separator lines left behind by a moved declaration.
			for line := lineForOffset(lineOffsets, start); line > 0 && start == lineOffsets[line]; line-- {
				previousStart := lineOffsets[line-1]
				if len(bytes.TrimSpace(code[previousStart:start])) != 0 {
					break
				}
				start = previousStart
			}
		}
		edits = append(edits, TextEdit{
			Range: Range{
				Start: positionForOffset(code, lineOffsets, start),
				End:   positionForOffset(code, lineOffsets, end),
			},
			NewText: replacement,
		})
	}
	if anchor != first.start {
		position := positionForOffset(code, lineOffsets, anchor)
		edits = append(edits, TextEdit{
			Range:   Range{Start: position, End: position},
			NewText: string(formatted) + lineEnding,
		})
	}
	return &CodeAction{
		Title:       "Merge top-level var declarations",
		Kind:        codeActionKindMergeVarDeclarations,
		Diagnostics: diagnostics,
		Edit:        &WorkspaceEdit{Changes: map[DocumentURI][]TextEdit{uri: edits}},
	}, nil
}
