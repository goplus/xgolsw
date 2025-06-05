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
	"go/types"

	"github.com/goplus/gop/token"
	"github.com/goplus/goxlsw/gop"
)

// InnermostScopeAt returns the innermost scope that contains the given
// position. It returns nil if not found.
func InnermostScopeAt(proj *gop.Project, pos token.Pos) *types.Scope {
	if !pos.IsValid() {
		return nil
	}
	_, typeInfo, _, _ := proj.TypeInfo()

	astFile := PosASTFile(proj, pos)
	if astFile == nil {
		return nil
	}

	fileScope := typeInfo.Scopes[astFile]
	if fileScope == nil {
		return nil
	}

	innermostScope := fileScope
	for _, scope := range typeInfo.Scopes {
		if scope.Contains(pos) && fileScope.Contains(scope.Pos()) && innermostScope.Contains(scope.Pos()) {
			innermostScope = scope
		}
	}
	return innermostScope
}
