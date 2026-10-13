package jsonrpc2

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotificationMarshalJSON(t *testing.T) {
	for _, tt := range []struct {
		name   string
		params any
		want   string
	}{
		{name: "NullParams", want: `{"jsonrpc":"2.0","method":"changed","params":null}`},
		{name: "ObjectParams", params: map[string]int{"version": 2}, want: `{"jsonrpc":"2.0","method":"changed","params":{"version":2}}`},
		{name: "ArrayParams", params: []int{1, 2}, want: `{"jsonrpc":"2.0","method":"changed","params":[1,2]}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			message, err := NewNotification("changed", tt.params)
			require.NoError(t, err)
			assert.Equal(t, "changed", message.Method())
			encoded, err := json.Marshal(message)
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(encoded))

			var direct Notification
			require.NoError(t, json.Unmarshal(encoded, &direct))
			decoded, err := DecodeMessage(encoded)
			require.NoError(t, err)
			notification, ok := decoded.(*Notification)
			require.True(t, ok)
			for _, got := range []*Notification{&direct, notification} {
				assert.Equal(t, message.Method(), got.Method())
				if tt.params == nil {
					assert.Empty(t, got.Params())
				} else {
					assert.JSONEq(t, string(message.Params()), string(got.Params()))
				}
			}
		})
	}
}

func TestCallMarshalJSON(t *testing.T) {
	for _, tt := range []struct {
		name string
		id   ID
		want string
	}{
		{name: "ZeroID", id: NewIntID(0), want: `{"jsonrpc":"2.0","id":0,"method":"hover","params":{"line":2}}`},
		{name: "NegativeID", id: NewIntID(-1), want: `{"jsonrpc":"2.0","id":-1,"method":"hover","params":{"line":2}}`},
		{name: "StringID", id: NewStringID("request"), want: `{"jsonrpc":"2.0","id":"request","method":"hover","params":{"line":2}}`},
		{name: "EmptyStringID", id: NewStringID(""), want: `{"jsonrpc":"2.0","id":"","method":"hover","params":{"line":2}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			message, err := NewCall(tt.id, "hover", map[string]int{"line": 2})
			require.NoError(t, err)
			assert.Equal(t, tt.id, message.ID())
			encoded, err := json.Marshal(message)
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(encoded))

			var direct Call
			require.NoError(t, json.Unmarshal(encoded, &direct))
			decoded, err := DecodeMessage(encoded)
			require.NoError(t, err)
			call, ok := decoded.(*Call)
			require.True(t, ok)
			for _, got := range []*Call{&direct, call} {
				assert.Equal(t, tt.id, got.ID())
				assert.Equal(t, message.Method(), got.Method())
				assert.JSONEq(t, string(message.Params()), string(got.Params()))
			}
		})
	}
}

func TestResponseMarshalJSON(t *testing.T) {
	for _, tt := range []struct {
		name   string
		result any
		err    error
		want   string
	}{
		{name: "NullResult", want: `{"jsonrpc":"2.0","id":"request","result":null}`},
		{
			name: "ObjectResult", result: map[string]int{"count": 2},
			want: `{"jsonrpc":"2.0","id":"request","result":{"count":2}}`,
		},
		{
			name: "WireError", result: "ignored",
			err:  &WireError{Code: -32602, Message: "invalid params", Data: new(json.RawMessage(`{"field":"name"}`))},
			want: `{"jsonrpc":"2.0","id":"request","error":{"code":-32602,"message":"invalid params","data":{"field":"name"}}}`,
		},
		{
			name: "PlainError", err: errors.New("failure"),
			want: `{"jsonrpc":"2.0","id":"request","error":{"code":0,"message":"failure"}}`,
		},
		{
			name: "WrappedWireError", err: fmt.Errorf("hover: %w", ErrInvalidParams),
			want: `{"jsonrpc":"2.0","id":"request","error":{"code":-32602,"message":"hover: JSON RPC invalid params"}}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			id := NewStringID("request")
			message, err := NewResponse(id, tt.result, tt.err)
			require.NoError(t, err)
			assert.Equal(t, id, message.ID())
			assert.Equal(t, tt.err, message.Err())
			encoded, err := json.Marshal(message)
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(encoded))

			var direct Response
			require.NoError(t, json.Unmarshal(encoded, &direct))
			decoded, err := DecodeMessage(encoded)
			require.NoError(t, err)
			response, ok := decoded.(*Response)
			require.True(t, ok)
			for _, got := range []*Response{&direct, response} {
				assert.Equal(t, id, got.ID())
				if tt.err == nil {
					require.NoError(t, got.Err())
					if tt.result == nil {
						assert.Empty(t, got.Result())
					} else {
						assert.JSONEq(t, string(message.Result()), string(got.Result()))
					}
				} else {
					require.EqualError(t, got.Err(), tt.err.Error())
					assert.Empty(t, got.Result())
					data, err := json.Marshal(got)
					require.NoError(t, err)
					assert.JSONEq(t, tt.want, string(data))
				}
			}
		})
	}
}

func TestDecodeMessage(t *testing.T) {
	t.Run("NotificationWithoutParams", func(t *testing.T) {
		message, err := DecodeMessage([]byte(`{"jsonrpc":"2.0","method":"initialized"}`))
		require.NoError(t, err)
		notification, ok := message.(*Notification)
		require.True(t, ok)
		assert.Equal(t, "initialized", notification.Method())
		assert.Empty(t, notification.Params())
	})

	t.Run("CallWithoutParams", func(t *testing.T) {
		message, err := DecodeMessage([]byte(`{"jsonrpc":"2.0","id":0,"method":"shutdown"}`))
		require.NoError(t, err)
		call, ok := message.(*Call)
		require.True(t, ok)
		assert.Equal(t, NewIntID(0), call.ID())
		assert.Equal(t, "shutdown", call.Method())
		assert.Empty(t, call.Params())
	})

	t.Run("MissingMethodAndID", func(t *testing.T) {
		message, err := DecodeMessage([]byte(`{"jsonrpc":"2.0","result":1}`))
		require.ErrorIs(t, err, ErrInvalidRequest)
		assert.Nil(t, message)
	})

	for _, tt := range []struct{ name, source string }{
		{"MalformedJSON", `{"jsonrpc":`},
		{"ArrayMessage", `[]`},
		{"WrongVersion", `{"jsonrpc":"1.0","method":"hover"}`},
		{"NumericVersion", `{"jsonrpc":2,"method":"hover"}`},
		{"InvalidID", `{"jsonrpc":"2.0","id":true,"method":"hover"}`},
		{"FractionalID", `{"jsonrpc":"2.0","id":1.5,"method":"hover"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			message, err := DecodeMessage([]byte(tt.source))
			require.ErrorContains(t, err, "unmarshaling jsonrpc message")
			assert.Nil(t, message)
		})
	}
}

func TestMessageJSONErrors(t *testing.T) {
	for _, tt := range []struct {
		name    string
		message interface {
			json.Marshaler
			json.Unmarshaler
		}
		wantUnmarshalErr string
	}{
		{
			name: "Notification", message: &Notification{params: json.RawMessage(`{`)},
			wantUnmarshalErr: "unmarshaling notification",
		},
		{
			name: "Call", message: &Call{params: json.RawMessage(`{`)},
			wantUnmarshalErr: "unmarshaling call",
		},
		{
			name: "Response", message: &Response{result: json.RawMessage(`{`)},
			wantUnmarshalErr: "unmarshaling jsonrpc response",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.message.MarshalJSON()
			var syntaxErr *json.SyntaxError
			require.ErrorAs(t, err, &syntaxErr)
			require.ErrorContains(t, tt.message.UnmarshalJSON([]byte(`[]`)), tt.wantUnmarshalErr)
		})
	}
}

func TestMessageConstructorsInvalidValues(t *testing.T) {
	t.Run("Notification", func(t *testing.T) {
		_, err := NewNotification("changed", func() {})
		var unsupported *json.UnsupportedTypeError
		require.ErrorAs(t, err, &unsupported)
	})

	t.Run("Call", func(t *testing.T) {
		_, err := NewCall(NewIntID(1), "hover", func() {})
		var unsupported *json.UnsupportedTypeError
		require.ErrorAs(t, err, &unsupported)
	})

	t.Run("Response", func(t *testing.T) {
		_, err := NewResponse(NewIntID(1), func() {}, nil)
		var unsupported *json.UnsupportedTypeError
		require.ErrorAs(t, err, &unsupported)
	})
}
