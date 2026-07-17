// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package agentfold

// snapshot_filter.go — the turn-snapshot view filter, mirroring the agents
// service's load-workspace.ts (keepInTurnSnapshot + the walk's skip rules).
// The Phase 4b BaseReader MUST apply this filter so the Go fold sees exactly
// the base the agents-side FileBundle was seeded with: a path the agents walk
// dropped must read as exists=false here, or an editFile against it succeeds
// on one side only and the folds diverge.

import (
	"bytes"
	"strings"
)

// KeepInTurnSnapshot mirrors keepInTurnSnapshot: keep agent-authored sources
// (*.md, *.dsl, *.cell, a design.json or validation-criteria.json basename) and
// drop everything else. *.cell is the project-level cell-diagram DSL
// (design.cell). validation-criteria.json is kept so a design regeneration can
// see the existing acceptance oracle and reuse its criterion ids (keeping
// committed e2e specs, which are keyed by criterion id, mapped) instead of
// renumbering.
func KeepInTurnSnapshot(path string) bool {
	if strings.HasSuffix(path, ".md") || strings.HasSuffix(path, ".dsl") || strings.HasSuffix(path, ".cell") {
		return true
	}
	base := path
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		base = path[i+1:]
	}
	return base == "design.json" || base == "validation-criteria.json"
}

// InTurnSnapshot is the complete per-file predicate the agents-side snapshot
// walk applies: dot-led path segments skipped at any depth, the keep-filter,
// and the NUL-byte binary skip. Phase 4b BaseReader implementations should
// return exists=false for any file failing this.
func InTurnSnapshot(path string, content []byte) bool {
	for _, seg := range strings.Split(path, "/") {
		if strings.HasPrefix(seg, ".") {
			return false
		}
	}
	if !KeepInTurnSnapshot(path) {
		return false
	}
	return !bytes.ContainsRune(content, 0)
}

// FilterTurnSnapshot mirrors filterTurnSnapshot: apply the walk's PATH rules
// to an in-memory map (values assumed text).
func FilterTurnSnapshot(files map[string]string) map[string]string {
	out := make(map[string]string, len(files))
	for path, content := range files {
		if !InTurnSnapshot(path, nil) {
			continue
		}
		out[path] = content
	}
	return out
}
