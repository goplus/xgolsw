package server

var (
	// generalCompletionSnippets contains statement snippets for XGo completion.
	generalCompletionSnippets = []symbolDefinition{
		{
			ID:       XGoDefinitionIdentifier{Name: new("for_iterate")},
			Overview: "for v in arr {}",
			Detail:   "Iterate within given set",

			CompletionItemLabel:            "for",
			CompletionItemInsertText:       "for ${1:v} in ${2:[]} {\n\t$0\n}",
			CompletionItemKind:             KeywordCompletion,
			CompletionItemInsertTextFormat: SnippetTextFormat,
		},
		{
			ID:       XGoDefinitionIdentifier{Name: new("for_iterate_with_index")},
			Overview: "for i, v in arr {}",
			Detail:   "Iterate with index within given set",

			CompletionItemLabel:            "for",
			CompletionItemInsertText:       "for ${1:i}, ${2:v} in ${3:[]} {\n\t$0\n}",
			CompletionItemKind:             KeywordCompletion,
			CompletionItemInsertTextFormat: SnippetTextFormat,
		},
		{
			ID:       XGoDefinitionIdentifier{Name: new("for_loop_with_condition")},
			Overview: "for condition {}",
			Detail:   "Loop with condition",

			CompletionItemLabel:            "for",
			CompletionItemInsertText:       "for ${1:true} {\n\t$0\n}",
			CompletionItemKind:             KeywordCompletion,
			CompletionItemInsertTextFormat: SnippetTextFormat,
		},
		{
			ID:       XGoDefinitionIdentifier{Name: new("for_loop_with_range")},
			Overview: "for i in start:end {}",
			Detail:   "Loop with range",

			CompletionItemLabel:            "for",
			CompletionItemInsertText:       "for ${1:i} in ${2:1}:${3:5} {\n\t$0\n}",
			CompletionItemKind:             KeywordCompletion,
			CompletionItemInsertTextFormat: SnippetTextFormat,
		},
		{
			ID:       XGoDefinitionIdentifier{Name: new("if_statement")},
			Overview: "if condition {}",
			Detail:   "If statement",

			CompletionItemLabel:            "if",
			CompletionItemInsertText:       "if ${1:true} {\n\t$0\n}",
			CompletionItemKind:             KeywordCompletion,
			CompletionItemInsertTextFormat: SnippetTextFormat,
		},
		{
			ID:       XGoDefinitionIdentifier{Name: new("if_else_statement")},
			Overview: "if condition {} else {}",
			Detail:   "If else statement",

			CompletionItemLabel:            "if",
			CompletionItemInsertText:       "if ${1:true} {\n\t$2\n} else {\n\t$0\n}",
			CompletionItemKind:             KeywordCompletion,
			CompletionItemInsertTextFormat: SnippetTextFormat,
		},
		{
			ID:       XGoDefinitionIdentifier{Name: new("var_declaration")},
			Overview: "var name type",
			Detail:   "Variable declaration, e.g., `var count int`",

			CompletionItemLabel:            "var",
			CompletionItemInsertText:       "var ${1:name} $0",
			CompletionItemKind:             KeywordCompletion,
			CompletionItemInsertTextFormat: SnippetTextFormat,
		},
	}

	// fileScopeCompletionSnippets contains declaration snippets available at file scope.
	fileScopeCompletionSnippets = []symbolDefinition{
		{
			ID:       XGoDefinitionIdentifier{Name: new("import_declaration")},
			Overview: "import \"package\"",
			Detail:   "Import package declaration, e.g., `import \"fmt\"`",

			CompletionItemLabel:            "import",
			CompletionItemInsertText:       "import \"${1:package}\"$0",
			CompletionItemKind:             KeywordCompletion,
			CompletionItemInsertTextFormat: SnippetTextFormat,
		},
		{
			ID:       XGoDefinitionIdentifier{Name: new("func_declaration")},
			Overview: "func name(params) { ... }",
			Detail:   "Function declaration, e.g., `func add(a int, b int) int {}`",

			CompletionItemLabel:            "func",
			CompletionItemInsertText:       "func ${1:name}(${2:params}) ${3:returnType} {\n\t$0\n}",
			CompletionItemKind:             KeywordCompletion,
			CompletionItemInsertTextFormat: SnippetTextFormat,
		},
	}
)
