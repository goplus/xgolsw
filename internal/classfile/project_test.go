package classfile

import (
	"testing"

	"github.com/goplus/xgolsw/internal/analysis"
	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProject(t *testing.T) {
	t.Run("NilBase", func(t *testing.T) {
		proj, err := NewProject(nil)
		assert.ErrorIs(t, err, errNilProject)
		assert.Nil(t, proj)
	})

	t.Run("ValidBase", func(t *testing.T) {
		base := xgo.NewProject(nil, nil, 0)
		proj, err := NewProject(base)
		require.NoError(t, err)
		require.NotNil(t, proj)
		assert.Same(t, base, proj.Underlying())
	})
}

func TestProjectSnapshot(t *testing.T) {
	withProviderRegistry(t, func() {
		t.Run("CachesProviderResults", func(t *testing.T) {
			provider := &recordingProvider{
				id: "test-provider",
				build: func(ctx *BuildContext) (*Snapshot, error) {
					return &Snapshot{Resources: &ResourceIndex{}}, nil
				},
			}
			RegisterProvider(provider)

			base := xgo.NewProject(nil, nil, 0)
			proj, err := NewProject(base)
			require.NoError(t, err)

			snap1, err := proj.Snapshot(provider.ID())
			require.NoError(t, err)
			require.NotNil(t, snap1)

			snap2, err := proj.Snapshot(provider.ID())
			require.NoError(t, err)
			require.NotNil(t, snap2)

			assert.Same(t, snap1, snap2)
			assert.Equal(t, 1, provider.CallCount())
		})
	})
}

func TestProjectSnapshotPropagatesAnalyzers(t *testing.T) {
	withProviderRegistry(t, func() {
		base := xgo.NewProject(nil, nil, 0)
		proj, err := NewProject(base)
		require.NoError(t, err)

		analyzer := requireDefaultAnalyzer(t)
		analyzers := []*analysis.Analyzer{analyzer}
		proj.SetAnalyzers(analyzers)
		analyzers[0] = nil

		var received []*analysis.Analyzer
		provider := &recordingProvider{
			id: "analyzer-provider",
			build: func(ctx *BuildContext) (*Snapshot, error) {
				received = ctx.Analyzers
				return &Snapshot{Resources: &ResourceIndex{}}, nil
			},
		}
		RegisterProvider(provider)

		_, err = proj.Snapshot(provider.ID())
		require.NoError(t, err)
		require.Len(t, received, 1)
		assert.Same(t, analyzer, received[0])
		assert.Nil(t, analyzers[0])
	})
}

func TestProjectSnapshotErrorCases(t *testing.T) {
	t.Run("NilReceiver", func(t *testing.T) {
		var proj *Project
		snapshot, err := proj.Snapshot("any")
		assert.Nil(t, snapshot)
		assert.ErrorIs(t, err, errNilProject)
	})

	t.Run("ProviderNotFound", func(t *testing.T) {
		base := xgo.NewProject(nil, nil, 0)
		proj, err := NewProject(base)
		require.NoError(t, err)

		snapshot, err := proj.Snapshot("missing")
		assert.Nil(t, snapshot)
		assert.ErrorIs(t, err, errProviderNotFound)
	})

	t.Run("NilSnapshotFromProvider", func(t *testing.T) {
		withProviderRegistry(t, func() {
			provider := &recordingProvider{
				id: "nil-snapshot",
				build: func(ctx *BuildContext) (*Snapshot, error) {
					return nil, nil
				},
			}
			RegisterProvider(provider)

			base := xgo.NewProject(nil, nil, 0)
			proj, err := NewProject(base)
			require.NoError(t, err)

			snapshot, err := proj.Snapshot(provider.ID())
			assert.Nil(t, snapshot)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "returned nil snapshot")
		})
	})
}

func TestProjectSnapshotForPath(t *testing.T) {
	t.Run("Match", func(t *testing.T) {
		withProviderRegistry(t, func() {
			provider := &recordingProvider{
				id:       "match",
				supports: func(path string) bool { return true },
				build: func(ctx *BuildContext) (*Snapshot, error) {
					return &Snapshot{Provider: "match"}, nil
				},
			}
			RegisterProvider(provider)

			base := xgo.NewProject(nil, nil, 0)
			proj, err := NewProject(base)
			require.NoError(t, err)

			snapshot, id, err := proj.SnapshotForPath("foo/bar.spx")
			require.NoError(t, err)
			require.NotNil(t, snapshot)
			assert.Equal(t, provider.ID(), id)
		})
	})

	t.Run("NoMatch", func(t *testing.T) {
		withProviderRegistry(t, func() {
			base := xgo.NewProject(nil, nil, 0)
			proj, err := NewProject(base)
			require.NoError(t, err)

			snapshot, id, err := proj.SnapshotForPath("foo/bar.spx")
			assert.Nil(t, snapshot)
			assert.Equal(t, ProviderID(""), id)
			assert.ErrorIs(t, err, errNoMatchingProvider)
		})
	})
}

func requireDefaultAnalyzer(t *testing.T) *analysis.Analyzer {
	t.Helper()
	for _, analyzer := range analysis.DefaultAnalyzers {
		return analyzer
	}
	t.Fatal("no analyzers registered")
	return nil
}
