package printf

import (
	_ "embed"
	"fmt"
	"go/constant"
	"go/types"
	"regexp"
	"strings"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/internal/analysis/ast/inspector"
	"github.com/goplus/xgolsw/internal/analysis/passes/inspect"
	"github.com/goplus/xgolsw/internal/analysis/passes/internal/analysisutil"
	"github.com/goplus/xgolsw/internal/analysis/passes/internal/typeutil"
	"github.com/goplus/xgolsw/internal/analysis/protocol"
)

//go:embed doc.go
var doc string

var Analyzer = &protocol.Analyzer{
	Name:     "printf",
	Doc:      analysisutil.MustExtractDoc(doc, "printf"),
	URL:      "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/printf",
	Requires: []*protocol.Analyzer{inspect.Analyzer},
	Run:      run,
}

// printfFuncs maps fully-qualified function names to whether they are printf-like (true) or print-like (false).
var printfFuncs = map[string]bool{
	"fmt.Fprintf":  true,
	"fmt.Printf":   true,
	"fmt.Sprintf":  true,
	"fmt.Errorf":   true,
	"fmt.Appendf":  true,
	"fmt.Fprintln": false,
	"fmt.Println":  false,
	"fmt.Print":    false,
	"fmt.Fprint":   false,
	"fmt.Sprint":   false,
	"fmt.Sprintln": false,
	"fmt.Append":   false,
	"fmt.Appendln": false,
	"log.Printf":   true,
	"log.Fatalf":   true,
	"log.Panicf":   true,
	"log.Print":    false,
	"log.Fatal":    false,
	"log.Panic":    false,
	"log.Println":  false,
}

// formatStringIndex returns the index of the format string argument for known printf functions,
// or -1 if not a printf-like function.
// For fprintf/sprintf style, the format string is after the writer argument.
var formatArgIndex = map[string]int{
	"fmt.Fprintf": 1,
	"fmt.Printf":  0,
	"fmt.Sprintf": 0,
	"fmt.Errorf":  0,
	"fmt.Appendf": 1,
	"log.Printf":  0,
	"log.Fatalf":  0,
	"log.Panicf":  0,
}

// printFormatRE matches possible format directives in a string.
var printFormatRE = regexp.MustCompile(`%` + `[+\-#]*` + `([0-9]+|\*)?` + `(\.[0-9]+)?` + `[bcdefgopqstvxEFGTUX]`)

func run(pass *protocol.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	nodeFilter := []ast.Node{
		(*ast.CallExpr)(nil),
	}
	insp.Preorder(nodeFilter, func(n ast.Node) {
		call := n.(*ast.CallExpr)

		fn := typeutil.StaticCallee(pass.TypesInfo, call)
		if fn == nil {
			return
		}

		fullName := fn.FullName()
		isPrintf, known := printfFuncs[fullName]
		if !known {
			return
		}

		if isPrintf {
			checkPrintf(pass, call, fullName)
		} else {
			checkPrint(pass, call, fullName)
		}
	})
	return nil, nil
}

// checkPrintf checks a call to a Printf-like function.
func checkPrintf(pass *protocol.Pass, call *ast.CallExpr, name string) {
	fmtIdx, ok := formatArgIndex[name]
	if !ok {
		return
	}
	if len(call.Args) <= fmtIdx {
		return
	}

	formatArg := call.Args[fmtIdx]
	firstDataArg := fmtIdx + 1

	// Check if the format string is a constant.
	tv := pass.TypesInfo.Types[formatArg]
	if tv.Value == nil {
		// Non-constant format string.
		// Common mistake: fmt.Printf(msg) with no args when msg contains %.
		if len(call.Args) == firstDataArg {
			pass.ReportRangef(formatArg, "non-constant format string in call to %s", name)
		}
		return
	}
	if tv.Value.Kind() != constant.String {
		return
	}

	format := constant.StringVal(tv.Value)

	if !strings.Contains(format, "%") {
		if len(call.Args) > firstDataArg {
			pass.ReportRangef(call, "%s call has arguments but no formatting directives", name)
		}
		return
	}

	// Count format verbs (excluding %%).
	nVerbs := countFormatVerbs(format)
	nArgs := len(call.Args) - firstDataArg

	// Don't flag variadic slice forwarding.
	if call.Ellipsis.IsValid() {
		return
	}

	if nArgs > nVerbs {
		pass.ReportRangef(call, "%s call needs %s but has %s",
			name, count(nVerbs, "arg"), count(nArgs, "arg"))
	} else if nArgs < nVerbs {
		pass.ReportRangef(call, "%s call needs %s but has %s",
			name, count(nVerbs, "arg"), count(nArgs, "arg"))
	}
}

// checkPrint checks a call to a Print-like (non-f) function.
func checkPrint(pass *protocol.Pass, call *ast.CallExpr, name string) {
	if len(call.Args) == 0 {
		return
	}

	// Determine first variadic argument.
	fn := pass.TypesInfo.Types[call.Fun].Type
	if fn == nil {
		return
	}
	sig, ok := fn.Underlying().(*types.Signature)
	if !ok || !sig.Variadic() {
		return
	}
	firstArg := sig.Params().Len() - 1
	if firstArg >= len(call.Args) {
		return
	}

	// Check if first argument looks like a format string.
	arg := call.Args[firstArg]
	tv := pass.TypesInfo.Types[arg]
	if tv.Value != nil && tv.Value.Kind() == constant.String {
		s := constant.StringVal(tv.Value)
		// Trim trailing % (common in URL-like strings)
		s = strings.TrimSuffix(s, "%")
		if strings.Contains(s, "%") {
			for _, m := range printFormatRE.FindAllString(s, -1) {
				// Allow %XX (URL percent-encoding)
				if len(m) >= 3 && isHex(m[1]) && isHex(m[2]) {
					continue
				}
				pass.ReportRangef(call, "%s call has possible Printf formatting directive %s", name, m)
				break
			}
		}
	}

	// Check last argument for trailing newline in Println.
	if strings.HasSuffix(name, "ln") {
		last := call.Args[len(call.Args)-1]
		ltv := pass.TypesInfo.Types[last]
		if ltv.Value != nil && ltv.Value.Kind() == constant.String {
			if s := constant.StringVal(ltv.Value); strings.HasSuffix(s, "\n") {
				pass.ReportRangef(call, "%s arg list ends with redundant newline", name)
			}
		}
	}
}

// countFormatVerbs counts the number of format verbs in a format string (excluding %%).
func countFormatVerbs(format string) int {
	n := 0
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}
		i++
		if i >= len(format) {
			break
		}
		if format[i] == '%' {
			continue // %%
		}
		// Skip flags
		for i < len(format) && strings.ContainsRune(" +-#0", rune(format[i])) {
			i++
		}
		// Skip width (optional)
		if i < len(format) && format[i] == '*' {
			n++ // * consumes an arg too
			i++
		} else {
			for i < len(format) && format[i] >= '0' && format[i] <= '9' {
				i++
			}
		}
		// Skip precision (optional)
		if i < len(format) && format[i] == '.' {
			i++
			if i < len(format) && format[i] == '*' {
				n++ // * consumes an arg too
				i++
			} else {
				for i < len(format) && format[i] >= '0' && format[i] <= '9' {
					i++
				}
			}
		}
		// The verb itself
		if i < len(format) {
			n++
		}
	}
	return n
}

func count(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return strings.TrimSpace(strings.Replace(
		strings.Replace("N whats", "N", fmt.Sprintf("%d", n), 1),
		"whats", what+"s", 1))
}

func isHex(b byte) bool {
	return '0' <= b && b <= '9' || 'A' <= b && b <= 'F' || 'a' <= b && b <= 'f'
}
