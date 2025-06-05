/*
 * Copyright (c) 2025 The GoPlus Authors (goplus.org). All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package goputil

import (
	"testing"

	"github.com/goplus/gop/ast"
	"github.com/goplus/gop/token"
	"github.com/goplus/goxlsw/gop"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInnermostScopeAt(t *testing.T) {
	proj := gop.NewProject(nil, map[string]gop.File{
		"main.gop": file(`
var x = 1

func test() {
	y := 2
	if true {
		z := 3
		println(x, y, z)
	}
}
`),
	}, gop.FeatAll)

	_, typeInfo, _, _ := proj.TypeInfo()
	require.NotNil(t, typeInfo)

	astFile, err := proj.AST("main.gop")
	require.NoError(t, err)
	require.Len(t, astFile.Decls, 2)
	astFileScope := typeInfo.Scopes[astFile]
	require.NotNil(t, astFileScope)

	for _, tt := range []struct {
		name    string
		pos     token.Pos
		wantNil bool
		wantVar string
	}{
		{
			name:    "CanSeeX",
			pos:     astFile.Decls[0].(*ast.GenDecl).Specs[0].(*ast.ValueSpec).Names[0].Pos(),
			wantVar: "x",
		},
		{
			name:    "CanSeeY",
			pos:     astFile.Decls[1].(*ast.FuncDecl).Body.Pos(),
			wantVar: "y",
		},
		{
			name: "CanSeeZ",
			pos: func() token.Pos {
				body := astFile.Decls[1].(*ast.FuncDecl).Body
				for _, stmt := range body.List {
					if ifStmt, ok := stmt.(*ast.IfStmt); ok {
						return ifStmt.Body.Pos()
					}
				}
				return token.NoPos
			}(),
			wantVar: "z",
		},
		{
			name:    "NotFound",
			pos:     token.NoPos,
			wantNil: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			scope := InnermostScopeAt(proj, tt.pos)
			if tt.wantNil {
				require.Nil(t, scope)
			} else {
				require.NotNil(t, scope)
				if scope == astFileScope {
					scope = scope.Parent()
				}
				if tt.wantVar != "" {
					assert.NotNil(t, scope.Lookup(tt.wantVar))
				}
			}
		})
	}
}
