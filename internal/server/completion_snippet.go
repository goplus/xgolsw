package server

var (
	// generalCompletionSnippets contains statement snippets for XGo completion.
	generalCompletionSnippets = []symbolDefinition{
		{
			ID:       XGoDefinitionIdentifier{Name: ToPtr("for_iterate")},
			Overview: "for v in arr {}",
			Detail:   "Iterate within given set",

			CompletionItemLabel:            "for",
			CompletionItemKind:             KeywordCompletion,
			CompletionItemInsertText:       "for ${1:v} in ${2:[]} {\n\t$0\n}",
			CompletionItemInsertTextFormat: SnippetTextFormat,
		},
		{
			ID:       XGoDefinitionIdentifier{Name: ToPtr("for_iterate_with_index")},
			Overview: "for i, v in arr {}",
			Detail:   "Iterate with index within given set",

			CompletionItemLabel:            "for",
			CompletionItemKind:             KeywordCompletion,
			CompletionItemInsertText:       "for ${1:i}, ${2:v} in ${3:[]} {\n\t$0\n}",
			CompletionItemInsertTextFormat: SnippetTextFormat,
		},
		{
			ID:       XGoDefinitionIdentifier{Name: ToPtr("for_loop_with_condition")},
			Overview: "for condition {}",
			Detail:   "Loop with condition",

			CompletionItemLabel:            "for",
			CompletionItemKind:             KeywordCompletion,
			CompletionItemInsertText:       "for ${1:true} {\n\t$0\n}",
			CompletionItemInsertTextFormat: SnippetTextFormat,
		},
		{
			ID:       XGoDefinitionIdentifier{Name: ToPtr("for_loop_with_range")},
			Overview: "for i in start:end {}",
			Detail:   "Loop with range",

			CompletionItemLabel:            "for",
			CompletionItemKind:             KeywordCompletion,
			CompletionItemInsertText:       "for ${1:i} in ${2:1}:${3:5} {\n\t$0\n}",
			CompletionItemInsertTextFormat: SnippetTextFormat,
		},
		{
			ID:       XGoDefinitionIdentifier{Name: ToPtr("if_statement")},
			Overview: "if condition {}",
			Detail:   "If statement",

			CompletionItemLabel:            "if",
			CompletionItemKind:             KeywordCompletion,
			CompletionItemInsertText:       "if ${1:true} {\n\t$0\n}",
			CompletionItemInsertTextFormat: SnippetTextFormat,
		},
		{
			ID:       XGoDefinitionIdentifier{Name: ToPtr("if_else_statement")},
			Overview: "if condition {} else {}",
			Detail:   "If else statement",

			CompletionItemLabel:            "if",
			CompletionItemKind:             KeywordCompletion,
			CompletionItemInsertText:       "if ${1:true} {\n\t$2\n} else {\n\t$0\n}",
			CompletionItemInsertTextFormat: SnippetTextFormat,
		},
		{
			ID:       XGoDefinitionIdentifier{Name: ToPtr("var_declaration")},
			Overview: "var name type",
			Detail:   "Variable declaration, e.g., `var count int`",

			CompletionItemLabel:            "var",
			CompletionItemKind:             KeywordCompletion,
			CompletionItemInsertText:       "var ${1:name} $0",
			CompletionItemInsertTextFormat: SnippetTextFormat,
		},
	}

	// fileScopeCompletionSnippets contains declaration snippets available at file scope.
	fileScopeCompletionSnippets = []symbolDefinition{
		{
			ID:       XGoDefinitionIdentifier{Name: ToPtr("import_declaration")},
			Overview: "import \"package\"",
			Detail:   "Import package declaration, e.g., `import \"fmt\"`",

			CompletionItemLabel:            "import",
			CompletionItemKind:             KeywordCompletion,
			CompletionItemInsertText:       "import \"${1:package}\"$0",
			CompletionItemInsertTextFormat: SnippetTextFormat,
		},
		{
			ID:       XGoDefinitionIdentifier{Name: ToPtr("func_declaration")},
			Overview: "func name(params) { ... }",
			Detail:   "Function declaration, e.g., `func add(a int, b int) int {}`",

			CompletionItemLabel:            "func",
			CompletionItemKind:             KeywordCompletion,
			CompletionItemInsertText:       "func ${1:name}(${2:params}) ${3:returnType} {\n\t$0\n}",
			CompletionItemInsertTextFormat: SnippetTextFormat,
		},
	}
)
