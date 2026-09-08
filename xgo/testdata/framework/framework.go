// Package framework provides a minimal classfile framework for Project tests.
package framework

const XGoPackage = true

// App is the project base class.
type App struct{}

func (a *App) initApp() {}

// OnStart accepts a project callback.
func (a *App) OnStart(callback func()) {}

// Measure__0 is the integer overload of Measure.
func (a *App) Measure__0(value int) int { return value }

// Measure__1 is the string overload of Measure.
func (a *App) Measure__1(value string) int { return len(value) }

// Item is the work base class.
type Item struct{}

// Main is the work entry point.
func (i *Item) Main() {}

// OnValue accepts a callback with an inferred integer parameter.
func (i *Item) OnValue(callback func(int)) {}

// Label is exposed as a property in XGo source.
func (i *Item) Label() string { return "item" }

// XGot_App_Main receives the generated project and work classes.
func XGot_App_Main(app interface{ initApp() }, items ...interface{ Main() }) {}
