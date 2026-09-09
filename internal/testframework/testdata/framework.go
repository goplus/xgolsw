// Package framework provides a minimal classfile framework for language tests.
package framework

const XGoPackage = true

// Low and High are values used by framework methods.
const (
	Low = iota
	High
)

// App is the project base class.
type App struct{}

func (a *App) initApp() {}

// OnStart accepts a project callback.
func (a *App) OnStart(callback func()) {}

// OnEvent__0 accepts an event callback without a value.
func (a *App) OnEvent__0(name string, callback func()) {}

// OnEvent__1 accepts an event callback with an inferred integer value.
func (a *App) OnEvent__1(name string, callback func(int)) {}

// Measure__0 is the integer overload of Measure.
func (a *App) Measure__0(value int) int { return value }

// Measure__1 is the string overload of Measure.
func (a *App) Measure__1(value string) int { return len(value) }

// RunWhen accepts a deferred condition and a callback.
func RunWhen(__xgo_autoclosure_condition func() bool, callback func()) {}

// XGot_App_XGox_Create provides a method with an explicit type argument.
func XGot_App_XGox_Create[T any](a *App, name string) *T { return nil }

// Item is the work base class.
type Item struct {
	// Value stores the work item's value.
	Value int
}

// Main is the work entry point.
func (i *Item) Main() {}

// OnValue accepts a callback with an inferred integer parameter.
func (i *Item) OnValue(callback func(int)) {}

// Label is exposed as a property in XGo source.
func (i *Item) Label() string { return "item" }

// Apply accepts a value on a work instance.
func (i *Item) Apply(value int) {}

// XGot_App_Main receives the generated project and work classes.
func XGot_App_Main(app interface{ initApp() }, items ...interface{ Main() }) {}
