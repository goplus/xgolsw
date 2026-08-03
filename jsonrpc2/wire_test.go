package jsonrpc2

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIDJSONRoundTrip(t *testing.T) {
	for _, tt := range []struct {
		name string
		json string
		want ID
	}{
		{name: "Number", json: `42`, want: NewIntID(42)},
		{name: "Zero", json: `0`, want: NewIntID(0)},
		{name: "String", json: `"request"`, want: NewStringID("request")},
		{name: "EmptyString", json: `""`, want: NewStringID("")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var got ID
			require.NoError(t, json.Unmarshal([]byte(tt.json), &got))
			assert.Equal(t, tt.want, got)

			encoded, err := json.Marshal(&got)
			require.NoError(t, err)
			assert.Equal(t, tt.json, string(encoded))
		})
	}
}

func TestIDKindsAreDistinct(t *testing.T) {
	ids := map[ID]struct{}{
		NewIntID(0):     {},
		NewStringID(""): {},
	}
	assert.Len(t, ids, 2)
}
