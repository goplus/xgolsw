package jsonrpc2

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToWireError(t *testing.T) {
	wire := &WireError{Code: -32602, Message: "invalid params", Data: new(json.RawMessage(`{"field":"name"}`))}
	for _, tt := range []struct {
		name string
		err  error
		want *WireError
	}{
		{name: "Nil"},
		{name: "Plain", err: errors.New("failure"), want: &WireError{Message: "failure"}},
		{name: "Direct", err: wire, want: wire},
		{name: "Wrapped", err: fmt.Errorf("request: %w", wire), want: &WireError{Code: wire.Code, Message: "request: invalid params"}},
		{name: "Joined", err: errors.Join(errors.New("failure"), wire), want: &WireError{Code: wire.Code, Message: "failure\ninvalid params"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, toWireError(tt.err))
		})
	}
	assert.Same(t, wire, toWireError(wire))
}

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

func TestMakeID(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value any
		want  ID
	}{
		{name: "Null"},
		{name: "Zero", value: float64(0), want: NewIntID(0)},
		{name: "Number", value: float64(42), want: NewIntID(42)},
		{name: "NegativeNumber", value: float64(-42), want: NewIntID(-42)},
		{name: "String", value: "request", want: NewStringID("request")},
		{name: "EmptyString", value: "", want: NewStringID("")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			id, err := MakeID(tt.value)
			require.NoError(t, err)
			assert.Equal(t, tt.want, id)
		})
	}

	for _, tt := range []struct {
		name  string
		value any
	}{
		{name: "Bool", value: true},
		{name: "Object", value: map[string]any{}},
		{name: "Array", value: []any{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := MakeID(tt.value)
			require.ErrorIs(t, err, ErrParse)
		})
	}
}

func TestIDFormat(t *testing.T) {
	for _, tt := range []struct {
		name   string
		id     ID
		plain  string
		quoted string
	}{
		{name: "Number", id: NewIntID(42), plain: "42", quoted: "#42"},
		{name: "Zero", id: NewIntID(0), plain: "0", quoted: "#0"},
		{name: "String", id: NewStringID("request"), plain: "request", quoted: `"request"`},
		{name: "EmptyString", id: NewStringID(""), quoted: `""`},
		{name: "EscapedString", id: NewStringID("a\nb"), plain: "a\nb", quoted: `"a\nb"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.plain, fmt.Sprintf("%v", tt.id))
			assert.Equal(t, tt.quoted, fmt.Sprintf("%q", tt.id))
		})
	}
}
