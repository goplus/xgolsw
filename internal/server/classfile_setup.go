package server

import (
	"sync"

	"github.com/goplus/xgolsw/internal/classfile"
	classfilespx "github.com/goplus/xgolsw/internal/classfile/spx"
)

var registerClassfileProvidersOnce sync.Once

func init() {
	registerClassfileProvidersOnce.Do(func() {
		classfile.RegisterProvider(classfilespx.NewProvider())
	})
}
