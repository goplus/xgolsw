package server

import (
	"bytes"
	"fmt"
	"go/types"
	"sync"
	"time"

	"github.com/goplus/gogen"
	gopast "github.com/goplus/gop/ast"
	gopscanner "github.com/goplus/gop/scanner"
	goptoken "github.com/goplus/gop/token"
	"github.com/goplus/goxlsw/gop"
	"github.com/goplus/goxlsw/jsonrpc2"
	"github.com/goplus/goxlsw/protocol"
	"github.com/qiniu/x/errors"
)

// didOpen handles the textDocument/didOpen notification from the LSP client.
// It updates the project with the new file content and publishes diagnostics.
// The document URI is converted to a filesystem path, and a file change is created
// with the document's content and version number.
func (s *Server) didOpen(params *DidOpenTextDocumentParams) error {
	path, err := s.fromDocumentURI(params.TextDocument.URI)
	if err != nil {
		return err
	}

	return s.didModifyFile([]gop.FileChange{{
		Path:    path,
		Content: []byte(params.TextDocument.Text),
		Version: int(params.TextDocument.Version),
	}})
}

// didChange handles the textDocument/didChange notification from the LSP client.
// It applies document changes to the project and publishes updated diagnostics.
// For simplicity, this implementation only uses the latest content change
// rather than applying incremental changes.
func (s *Server) didChange(params *DidChangeTextDocumentParams) error {
	path, err := s.fromDocumentURI(params.TextDocument.URI)
	if err != nil {
		return err
	}

	content, err := s.changedText(path, params.ContentChanges)
	if err != nil {
		return err
	}

	// Create a file change record
	changes := []gop.FileChange{{
		Path:    path,
		Content: content,
		Version: int(params.TextDocument.Version),
	}}

	return s.didModifyFile(changes)
}

// didSave handles the textDocument/didSave notification from the LSP client.
// If the notification includes the document text, the project is updated.
// Otherwise, no change is made since the document content hasn't changed.
// Save notifications typically don't include version numbers, so 0 is used.
func (s *Server) didSave(params *DidSaveTextDocumentParams) error {
	// If text is included in save notification, update the file
	if params.Text != nil {
		path, err := s.fromDocumentURI(params.TextDocument.URI)
		if err != nil {
			return err
		}

		return s.didModifyFile([]gop.FileChange{{
			Path:    path,
			Content: []byte(*params.Text),
			Version: int(time.Now().UnixMilli()),
		}})
	}
	return nil
}

// didClose handles the textDocument/didClose notification from the LSP client.
// When a document is closed, its diagnostics are cleared by sending an empty
// diagnostics array to the client.
func (s *Server) didClose(params *DidCloseTextDocumentParams) error {
	// Clear diagnostics when file is closed
	return s.publishDiagnostics(params.TextDocument.URI, nil)
}

// didModifyFile is a shared implementation for handling document modifications.
// It updates the project with file changes and asynchronously publishes diagnostics.
// The function:
// 1. Updates the project's files with the provided changes
// 2. Starts a goroutine to generate and publish diagnostics for each changed file
// 3. Returns immediately after updating files for better responsiveness
func (s *Server) didModifyFile(changes []gop.FileChange) error {
	// 1. Update files synchronously
	s.getProj().ModifyFiles(changes)

	// 2. Asynchronously generate and publish diagnostics
	// This allows for quick response while diagnostics computation happens in background
	go func() {
		for _, change := range changes {
			// Convert path to URI for diagnostics
			uri := s.toDocumentURI(change.Path)

			// Get diagnostics from AST and type checking
			diagnostics, err := s.getDiagnostics(change.Path)
			if err != nil {
				// Log error but continue processing other files
				continue
			}

			// Publish diagnostics
			if err := s.publishDiagnostics(uri, diagnostics); err != nil {
				// Log error but continue
				continue
			}
		}
	}()

	return nil
}

// changedText processes document content changes from the client.
// It supports two modes of operation:
// 1. Full replacement: Replace the entire document content (when only one change with no range is provided)
// 2. Incremental updates: Apply specific changes to portions of the document
//
// Returns the updated document content or an error if the changes couldn't be applied.
func (s *Server) changedText(uri string, changes []protocol.TextDocumentContentChangeEvent) ([]byte, error) {
	if len(changes) == 0 {
		return nil, fmt.Errorf("%w: no content changes provided", jsonrpc2.ErrInternal)
	}

	// Check if the client sent the full content of the file.
	// We accept a full content change even if the server expected incremental changes.
	if len(changes) == 1 && changes[0].Range == nil && changes[0].RangeLength == 0 {
		// Full replacement mode
		return []byte(changes[0].Text), nil
	}

	// Incremental update mode
	return s.applyIncrementalChanges(uri, changes)
}

// applyIncrementalChanges applies a sequence of changes to the document content.
// For each change, it:
// 1. Computes the byte offsets for the specified range
// 2. Verifies the range is valid
// 3. Replaces the specified range with the new text
//
// Returns the updated document content or an error if the changes couldn't be applied.
func (s *Server) applyIncrementalChanges(path string, changes []protocol.TextDocumentContentChangeEvent) ([]byte, error) {
	// Get current file content
	file, ok := s.getProj().File(path)
	if !ok {
		return nil, fmt.Errorf("%w: file not found", jsonrpc2.ErrInternal)
	}

	content := file.Content

	// Apply each change sequentially
	for _, change := range changes {
		// Ensure the change includes range information
		if change.Range == nil {
			return nil, fmt.Errorf("%w: unexpected nil range for change", jsonrpc2.ErrInternal)
		}

		// Convert LSP positions to byte offsets
		start := s.PositionOffset(content, change.Range.Start)
		end := s.PositionOffset(content, change.Range.End)

		// Validate range
		if end < start {
			return nil, fmt.Errorf("%w: invalid range for content change", jsonrpc2.ErrInternal)
		}

		// Apply the change
		var buf bytes.Buffer
		buf.Write(content[:start])
		buf.WriteString(change.Text)
		buf.Write(content[end:])
		content = buf.Bytes()
	}

	return content, nil
}

// PositionOffset converts an LSP position (line, character) to a byte offset in the document.
// It calculates the offset by:
// 1. Finding the starting byte offset of the requested line
// 2. Adding the character offset within that line, converting from UTF-16 to UTF-8 if needed
//
// Parameters:
// - content: The file content as a byte array
// - position: The LSP position with line and character numbers (0-based)
//
// Returns the byte offset from the beginning of the document
func (s *Server) PositionOffset(content []byte, position Position) int {
	// If content is empty or position is beyond the content, return 0
	if len(content) == 0 {
		return 0
	}

	// Find all line start positions in the document
	lineStarts := []int{0} // First line always starts at position 0
	for i := 0; i < len(content); i++ {
		if content[i] == '\n' {
			lineStarts = append(lineStarts, i+1) // Next line starts after the newline
		}
	}

	// Ensure the requested line is within range
	lineIndex := int(position.Line)
	if lineIndex >= len(lineStarts) {
		// If line is beyond available lines, return the end of content
		return len(content)
	}

	// Get the starting offset of the requested line
	lineOffset := lineStarts[lineIndex]

	// Extract the content of the requested line
	lineEndOffset := len(content)
	if lineIndex+1 < len(lineStarts) {
		lineEndOffset = lineStarts[lineIndex+1] - 1 // -1 to exclude the newline character
	}

	// Ensure we don't go beyond the end of content
	if lineOffset >= len(content) {
		return len(content)
	}

	lineContent := content[lineOffset:min(lineEndOffset, len(content))]

	// Convert UTF-16 character offset to UTF-8 byte offset
	utf8Offset := utf16OffsetToUTF8(string(lineContent), int(position.Character))

	// Ensure the final offset doesn't exceed the content length
	return lineOffset + utf8Offset
}

var mu sync.Mutex

// getDiagnostics generates diagnostic information for a specific file.
// It performs two checks:
// 1. AST parsing - reports syntax errors
// 2. Type checking - reports type errors
//
// If AST parsing fails, only syntax errors are returned as diagnostics.
// If AST parsing succeeds but type checking fails, type errors are returned.
// Returns a slice of diagnostics and an error (if diagnostic generation failed).
func (s *Server) getDiagnostics(path string) ([]Diagnostic, error) {
	var diagnostics []Diagnostic
	mu.Lock()
	defer mu.Unlock()

	proj := s.getProj()

	// 1. Get AST diagnostics
	// Parse the file and check for syntax errors
	astFile, err := proj.AST(path)
	if err != nil {
		var (
			errorList gopscanner.ErrorList
			codeError *gogen.CodeError
		)
		if errors.As(err, &errorList) {
			// Handle parse errors.
			for _, e := range errorList {
				diagnostics = append(diagnostics, Diagnostic{
					Severity: SeverityError,
					Range:    s.rangeForASTFilePosition(astFile, e.Pos),
					Message:  e.Msg,
				})
			}
		} else if errors.As(err, &codeError) {
			// Handle code generation errors.
			diagnostics = append(diagnostics, Diagnostic{
				Severity: SeverityError,
				Range:    s.rangeForPos(codeError.Pos),
				Message:  codeError.Error(),
			})
		} else {
			// Handle unknown errors (including recovered panics).
			diagnostics = append(diagnostics, Diagnostic{
				Severity: SeverityError,
				Message:  fmt.Sprintf("failed to parse spx file: %v", err),
			})
		}
	}

	if astFile == nil {
		return diagnostics, nil
	}

	handleErr := func(err error) {
		if typeErr, ok := err.(types.Error); ok {
			diagnostics = append(diagnostics, Diagnostic{
				Severity: SeverityError,
				Range:    s.rangeForPos(typeErr.Pos),
				Message:  typeErr.Msg,
			})
		}
	}

	// 2. Get type checking diagnostics
	// Perform type checking on the file
	_, _, err, _ = proj.TypeInfo()
	if err != nil {
		// Add type checking errors to diagnostics
		switch err := err.(type) {
		case errors.List:
			for _, e := range err {
				handleErr(e)
			}
		default:
			handleErr(err)
		}
	}

	return diagnostics, nil
}

// fromPosition converts a token.Position to an LSP Position.
func (s *Server) fromPosition(astFile *gopast.File, position goptoken.Position) Position {
	tokenFile := s.getProj().Fset.File(astFile.Pos())

	line := position.Line
	lineStart := int(tokenFile.LineStart(line))
	relLineStart := lineStart - tokenFile.Base()
	lineContent := astFile.Code[relLineStart : relLineStart+position.Column-1]
	utf16Offset := utf8OffsetToUTF16(string(lineContent), position.Column-1)

	return Position{
		Line:      uint32(position.Line - 1),
		Character: uint32(utf16Offset),
	}
}

// rangeForASTFilePosition returns a [Range] for the given position in an AST file.
func (s *Server) rangeForASTFilePosition(astFile *gopast.File, position goptoken.Position) Range {
	p := s.fromPosition(astFile, position)
	return Range{Start: p, End: p}
}

// rangeForPos returns the [Range] for the given position.
func (s *Server) rangeForPos(pos goptoken.Pos) Range {
	return s.rangeForASTFilePosition(s.posASTFile(pos), s.getProj().Fset.Position(pos))
}

// posASTFile returns the AST file for the given position.
func (s *Server) posASTFile(pos goptoken.Pos) *gopast.File {
	return getASTPkg(s.getProj()).Files[s.posFilename(pos)]
}

// posFilename returns the filename for the given position.
func (s *Server) posFilename(pos goptoken.Pos) string {
	return s.getProj().Fset.Position(pos).Filename
}
