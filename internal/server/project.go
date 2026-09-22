package server

import (
	"maps"

	"github.com/goplus/xgolsw/xgo"
)

// providerSnapshot records the invocation order and files from a provider read.
type providerSnapshot struct {
	sequence uint64
	files    map[string]*xgo.File
}

// readProviderFiles invokes the provider outside projectMu so callbacks may
// reenter the server. Sequence numbers keep delayed reads from undoing newer ones.
func (s *Server) readProviderFiles() providerSnapshot {
	sequence := s.providerSequence.Add(1)
	return providerSnapshot{sequence: sequence, files: s.fileMapGetter()}
}

// getProj returns the mutable workspace used for document updates.
func (s *Server) getProj() *xgo.Project {
	return s.workspaceRootFS
}

// requestProject synchronizes source files and finishes type checking before
// requests can read syntax that the compiler mutates. Requests for the same
// revision share the snapshot and its analysis caches.
func (s *Server) requestProject() *xgo.Project {
	proj := s.syncProject()
	proj.TypeInfo()
	return proj
}

// syncProject refreshes provider files and returns a stable source snapshot.
// Provider callbacks and analysis run without holding the document state lock.
func (s *Server) syncProject() *xgo.Project {
	files := s.readProviderFiles()
	s.projectMu.Lock()
	defer s.projectMu.Unlock()
	s.syncFilesLocked(files)
	return s.projectSnapshotLocked()
}

// syncFilesLocked merges provider files with open documents. The caller holds projectMu.
func (s *Server) syncFilesLocked(snapshot providerSnapshot) {
	if snapshot.sequence > s.providerSnapshot.sequence {
		s.providerSnapshot = providerSnapshot{sequence: snapshot.sequence, files: maps.Clone(snapshot.files)}
	}
	provided := make(map[string]*xgo.File, len(s.providerSnapshot.files)+len(s.openFiles))
	maps.Copy(provided, s.providerSnapshot.files)
	maps.Copy(provided, s.openFiles)
	proj := s.getProj()
	revision := proj.Revision()
	proj.UpdateFiles(provided)
	if proj.Revision() != revision {
		s.queueOpenDiagnosticsLocked()
	}
}

// projectSnapshotLocked reuses the snapshot until workspace files or configuration
// change. Each revision owns its file set so old source positions are retained
// only by requests using that revision. The caller holds projectMu. Returned
// snapshots must not be modified.
func (s *Server) projectSnapshotLocked() *xgo.Project {
	proj := s.getProj()
	if revision := proj.Revision(); s.analysisSnapshot == nil || s.analysisRevision != revision {
		s.analysisSnapshot = proj.Fork()
		s.analysisRevision = revision
	}
	return s.analysisSnapshot
}
