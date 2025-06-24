package server

import (
	"github.com/goplus/xgolsw/internal/vfs"
	"github.com/goplus/xgolsw/xgo"
)

func newMapFSWithoutModTime(files map[string][]byte) *vfs.MapFS {
	fileMap := make(map[string]*vfs.MapFile)
	for k, v := range files {
		fileMap[k] = &vfs.MapFile{Content: v}
	}
	return xgo.NewProject(nil, fileMap, xgo.FeatAll)
}

func fileMapGetter(files map[string][]byte) func() map[string]*vfs.MapFile {
	return func() map[string]*vfs.MapFile {
		fileMap := make(map[string]*vfs.MapFile)
		for k, v := range files {
			fileMap[k] = &vfs.MapFile{Content: v}
		}
		return fileMap
	}
}
