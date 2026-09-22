// Package fmt supplies the declarations required to initialize XGo's builtins.
package fmt

type Writer interface {
	Write([]byte) (int, error)
}

func Print(args ...any) (int, error)                            { return 0, nil }
func Println(args ...any) (int, error)                          { return 0, nil }
func Printf(format string, args ...any) (int, error)            { return 0, nil }
func Errorf(format string, args ...any) error                   { return nil }
func Fprint(w Writer, args ...any) (int, error)                 { return 0, nil }
func Fprintln(w Writer, args ...any) (int, error)               { return 0, nil }
func Fprintf(w Writer, format string, args ...any) (int, error) { return 0, nil }
func Sprint(args ...any) string                                 { return "" }
func Sprintln(args ...any) string                               { return "" }
func Sprintf(format string, args ...any) string                 { return "" }
