package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestXGoDefinitionIdentifierString(t *testing.T) {
	for _, tt := range []struct {
		name string
		id   XGoDefinitionIdentifier
		want string
	}{
		{name: "Empty", want: "xgo:"},
		{
			name: "PackageOnly",
			id:   XGoDefinitionIdentifier{Package: new("example.com/sprites")},
			want: "xgo:example.com/sprites",
		},
		{
			name: "NameOnly",
			id:   XGoDefinitionIdentifier{Name: new("read +?")},
			want: "xgo:?read+%2B%3F",
		},
		{
			name: "Overload",
			id:   XGoDefinitionIdentifier{Package: new("example.com/sprites"), Name: new("Sprite.move"), OverloadID: new("0#?")},
			want: "xgo:example.com/sprites?Sprite.move#0%23%3F",
		},
		{
			name: "OverloadWithoutName",
			id:   XGoDefinitionIdentifier{Package: new("main"), OverloadID: new("0")},
			want: "xgo:main",
		},
		{
			name: "PresentEmptyParts",
			id:   XGoDefinitionIdentifier{Package: new(""), Name: new(""), OverloadID: new("")},
			want: "xgo:?#",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.id.String())
		})
	}
}
