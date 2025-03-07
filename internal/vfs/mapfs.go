package vfs

import (
	"io/fs"
	"sort"
	"strings"

	"github.com/goplus/goxlsw/gop"
	xfs "github.com/qiniu/x/http/fs"
)

type MapFile = gop.File
type MapFS = gop.Project

// WithOverlay returns a new MapFS with overlay files.
func WithOverlay(rootFS *MapFS, overlay map[string]MapFile) *MapFS {
	ret := rootFS.Snapshot()
	for k, v := range overlay {
		ret.PutFile(k, v)
	}
	return ret
}

// ListSpxFiles returns a list of .spx files in the rootFS.
func ListSpxFiles(rootFS *MapFS) (files []string, err error) {
	rootFS.RangeFiles(func(path string) bool {
		if strings.HasSuffix(path, ".spx") {
			files = append(files, path)
		}
		return true
	})
	return
}

// ReadFile reads a file from the rootFS.
func ReadFile(rootFS *MapFS, name string) ([]byte, error) {
	ret, ok := rootFS.File(name)
	if !ok {
		return nil, gop.ErrNotFound
	}
	return ret.Content, nil
}

type SubFS struct {
	root *MapFS
	base string
}

func (fs SubFS) ReadFile(name string) ([]byte, error) {
	return ReadFile(fs.root, fs.base+"/"+name)
}

func (fs SubFS) Readdir(name string) (ret []fs.FileInfo, err error) {
	prefix := fs.base + "/" + name + "/"
	entries := map[string]int{}
	fs.root.RangeFileContents(func(path string, file gop.File) bool {
		if strings.HasPrefix(path, prefix) {
			name := path[len(prefix):]
			if i := strings.Index(name, "/"); i >= 0 {
				entries[name[:i]] = -1
			} else {
				entries[name] = len(file.Content)
			}
		}
		return true
	})
	for name, size := range entries {
		if size < 0 {
			ret = append(ret, xfs.NewDirInfo(name))
		} else {
			ret = append(ret, xfs.NewFileInfo(name, int64(size)))
		}
	}
	sort.Slice(ret, func(i, j int) bool {
		return ret[i].Name() < ret[j].Name()
	})
	return
}

func Sub(rootFS *MapFS, base string) SubFS {
	return SubFS{rootFS, base}
}
