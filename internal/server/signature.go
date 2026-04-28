package server

import (
	gotypes "go/types"
	"strings"

	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_signatureHelp
func (s *Server) textDocumentSignatureHelp(params *SignatureHelpParams) (*SignatureHelp, error) {
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
	ident := xgoutil.IdentAtPosition(result.proj.Fset, typeInfo, astFile, position)

	obj := typeInfo.ObjectOf(ident)
	if obj == nil {
		return nil, nil
	}

	fun, ok := obj.(*gotypes.Func)
	if !ok {
		return nil, nil
	}

	return &SignatureHelp{
		Signatures: []SignatureInformation{signatureHelpInformation(fun)},
	}, nil
}

// signatureHelpInformation returns signature information for fun.
func signatureHelpInformation(fun *gotypes.Func) SignatureInformation {
	sig := fun.Signature()
	_, displayedName, _, isXGotMethod := displayedFuncName(fun)
	paramLabels := displayedFuncParamLabels(sig, isXGotMethod)
	paramInfos := make([]ParameterInformation, 0, len(paramLabels))
	for _, paramLabel := range paramLabels {
		paramInfos = append(paramInfos, ParameterInformation{
			Label: paramLabel,
			// TODO: Add documentation.
		})
	}

	return SignatureInformation{
		Label:      displayedName + "(" + strings.Join(paramLabels, ", ") + ")" + displayedFuncResults(sig.Results()),
		Parameters: paramInfos,
	}
}
