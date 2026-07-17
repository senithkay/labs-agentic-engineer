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

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

var ctx = context.Background()

func mustOK(t *testing.T, res OpResult, err error, status OpStatus) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.OK || res.Status != status {
		t.Fatalf("want ok/%s, got %+v", status, res)
	}
}

func mustErrCode(t *testing.T, res OpResult, err error, code ErrCode) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.OK || res.Code != code {
		t.Fatalf("want error code %s, got %+v", code, res)
	}
}

func TestEditFile_UnicodeAndCRLF(t *testing.T) {
	// Seed carries CRLF and multibyte content; the fold canonicalizes to LF
	// and matches literally (no structural normalization).
	seed := map[string]string{
		"specs/notes.md": "# Überschrift\r\nCafé line one\r\n漢字 🎉 emoji line\r\n",
	}
	f := NewFromSnapshot(seed)
	res, err := f.EditFile(ctx, "specs/notes.md", "Café line one\r\n漢字 🎉", "Café line ONE\n漢字 🎉🎉")
	mustOK(t, res, err, StatusApplied)
	got := f.FullState(seed)["specs/notes.md"]
	want := "# Überschrift\nCafé line ONE\n漢字 🎉🎉 emoji line\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestEditFile_NotUniqueCandidates(t *testing.T) {
	seed := map[string]string{"a.md": "alpha x\nbeta\n  alpha x\ngamma\nalpha x end\n"}
	f := NewFromSnapshot(seed)
	res, err := f.EditFile(ctx, "a.md", "alpha x", "ALPHA")
	mustErrCode(t, res, err, ErrNotUnique)
	if res.Count != 3 {
		t.Fatalf("count = %d, want 3", res.Count)
	}
	want := []MatchCandidate{{1, "alpha x"}, {3, "alpha x"}, {5, "alpha x end"}}
	if len(res.Candidates) != len(want) {
		t.Fatalf("candidates = %+v", res.Candidates)
	}
	for i, w := range want {
		if res.Candidates[i] != w {
			t.Errorf("candidate[%d] = %+v, want %+v", i, res.Candidates[i], w)
		}
	}
	// State untouched.
	if len(f.Touched()) != 0 {
		t.Fatal("rejected op must not touch")
	}
}

func TestEditFile_CandidateCapAt6(t *testing.T) {
	seed := map[string]string{"a.md": strings.Repeat("dup line\n", 9)}
	f := NewFromSnapshot(seed)
	res, err := f.EditFile(ctx, "a.md", "dup line", "x")
	mustErrCode(t, res, err, ErrNotUnique)
	if res.Count != 9 || len(res.Candidates) != 6 {
		t.Fatalf("count=%d candidates=%d, want 9/6", res.Count, len(res.Candidates))
	}
}

func TestEditFile_AlreadyAppliedIdempotence(t *testing.T) {
	seed := map[string]string{"a.md": "hello old world\n"}
	f := NewFromSnapshot(seed)
	res, err := f.EditFile(ctx, "a.md", "old world", "new world")
	mustOK(t, res, err, StatusApplied)
	// The duplicated tool call: oldString gone, newString present exactly once.
	res, err = f.EditFile(ctx, "a.md", "old world", "new world")
	mustOK(t, res, err, StatusAlreadyApplied)
	// A newString that is NOT unique in the file is a genuine NOT_FOUND
	// (the loose "substring present" test would mask it) …
	res, err = f.EditFile(ctx, "a.md", "absent", "o")
	mustErrCode(t, res, err, ErrNotFound)
	// … and so is a whitespace-only newString.
	res, err = f.EditFile(ctx, "a.md", "absent", " ")
	mustErrCode(t, res, err, ErrNotFound)
}

func TestEditFile_NotFoundClosestLines(t *testing.T) {
	seed := map[string]string{"a.md": "# Title\nsome interesting line here\nother\n"}
	f := NewFromSnapshot(seed)
	res, err := f.EditFile(ctx, "a.md", "some interesting line WRONG", "x")
	mustErrCode(t, res, err, ErrNotFound)
	if len(res.Candidates) != 1 || res.Candidates[0].Line != 2 {
		t.Fatalf("closest-lines echo missing: %+v", res.Candidates)
	}
}

func TestEditFile_EmptyOldStringAndNoSuchFile(t *testing.T) {
	f := NewFromSnapshot(map[string]string{"a.md": "x"})
	res, err := f.EditFile(ctx, "a.md", "", "y")
	mustErrCode(t, res, err, ErrEmptyOldString)
	res, err = f.EditFile(ctx, "missing.md", "a", "b")
	mustErrCode(t, res, err, ErrNoSuchFile)
}

func TestAddFile_EmptyContentAndInvalidPath(t *testing.T) {
	seed := map[string]string{}
	f := NewFromSnapshot(seed)
	res, err := f.AddFile(ctx, "specs/empty.md", "")
	mustOK(t, res, err, StatusApplied)
	if got := f.FullState(seed)["specs/empty.md"]; got != "" {
		t.Fatalf("empty add: %q", got)
	}
	// sha256("") — the TS manifest value for an empty file.
	if h := sha256Hex(""); h != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("sha256 of empty = %s", h)
	}
	res, err = f.AddFile(ctx, "   ", "x")
	mustErrCode(t, res, err, ErrInvalidPath)
}

func TestAddFile_NoopAndAlreadyExists(t *testing.T) {
	f := NewFromSnapshot(map[string]string{"a.md": "same\n"})
	res, err := f.AddFile(ctx, "a.md", "same\r\n") // CRLF canonicalizes to identical
	mustOK(t, res, err, StatusNoop)
	if len(f.Touched()) != 0 {
		t.Fatal("noop must not touch")
	}
	res, err = f.AddFile(ctx, "a.md", "different\n")
	mustErrCode(t, res, err, ErrAlreadyExists)
}

func TestRemoveFile_ProtectedPathsRefused(t *testing.T) {
	f := NewFromSnapshot(map[string]string{
		"specs/requirements/requirements.md": "req",
		"specs/design/design.md":             "des",
	})
	for _, p := range []string{"specs/requirements/requirements.md", "specs/design/design.md"} {
		res, err := f.RemoveFile(ctx, p)
		mustErrCode(t, res, err, ErrProtectedPath)
	}
	if len(f.Touched()) != 0 {
		t.Fatal("refused removes must not touch")
	}
	// Idempotent delete of an absent path is a clean noop.
	res, err := f.RemoveFile(ctx, "never/was.md")
	mustOK(t, res, err, StatusNoop)
}

func TestDeleteThenRecreateSamePath(t *testing.T) {
	seed := map[string]string{"specs/x.md": "v1\n"}
	f := NewFromSnapshot(seed)
	res, err := f.RemoveFile(ctx, "specs/x.md")
	mustOK(t, res, err, StatusApplied)
	res, err = f.AddFile(ctx, "specs/x.md", "v2\n")
	mustOK(t, res, err, StatusApplied)
	state := f.FullState(seed)
	if state["specs/x.md"] != "v2\n" {
		t.Fatalf("recreate lost: %q", state["specs/x.md"])
	}
	touched := f.Touched()
	if c := touched["specs/x.md"]; c == nil || *c != "v2\n" {
		t.Fatalf("touched final content wrong: %v", c)
	}
	// Manifest view: present with the sha of v2 — matches the TS bundle.
	if err := Verify(f, Manifest{Files: map[string]string{"specs/x.md": sha256Hex("v2\n")}}); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestAddThenRemove_NeverInSeed(t *testing.T) {
	// The degenerate touched-then-absent path lands in deleted[] and folds as
	// a no-op on a tree that never had it.
	f := NewFromSnapshot(map[string]string{})
	res, err := f.AddFile(ctx, "specs/tmp.md", "x")
	mustOK(t, res, err, StatusApplied)
	res, err = f.RemoveFile(ctx, "specs/tmp.md")
	mustOK(t, res, err, StatusApplied)
	if err := Verify(f, Manifest{Files: map[string]string{}, Deleted: []string{"specs/tmp.md"}}); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestYAMLGuard_RejectionLeavesFoldUnchanged(t *testing.T) {
	seed := map[string]string{
		"cfg.yaml": "a: 1\nb: 2\n",
		"doc.md":   "---\ntitle: T\n---\nBody\n",
	}
	f := NewFromSnapshot(seed)
	res, err := f.EditFile(ctx, "cfg.yaml", "b: 2", "b: [unclosed")
	mustErrCode(t, res, err, ErrInvalidYAML)
	res, err = f.EditFile(ctx, "doc.md", "title: T", "title: [broken")
	mustErrCode(t, res, err, ErrInvalidYAML)
	if len(f.Touched()) != 0 {
		t.Fatal("guard rejections must not touch")
	}
	state := f.FullState(seed)
	if state["cfg.yaml"] != seed["cfg.yaml"] || state["doc.md"] != seed["doc.md"] {
		t.Fatal("guard rejection mutated the fold")
	}
	// Duplicate keys and multi-doc are npm-parse errors too.
	res, err = f.AddFile(ctx, "dup.yaml", "a: 1\na: 2\n")
	mustErrCode(t, res, err, ErrInvalidYAML)
	res, err = f.AddFile(ctx, "multi.yaml", "a: 1\n---\nb: 2\n")
	mustErrCode(t, res, err, ErrInvalidYAML)
}

func TestComponentDesignGate(t *testing.T) {
	t.Skip("DISABLED pending fixture migration to the dependencies[] schema " +
		"(dependency-management follow-up): this fixture uses the pre-Phase-3 connections[] " +
		"schema. See docs/design/dependency-management-migration.md §8 (Phase-6 e2e Bug A).")
	const path = "specs/design/components/api-service/design.json"
	valid := `{"name":"api-service","type":"service","version":"1.0","language":"go",` +
		`"buildpack":"go","appPath":".","entrypoint":"main.go","exposure":"internet",` +
		`"connections":[{"to":"db","type":"datastore"}],"description":"The API."}`
	f := NewFromSnapshot(map[string]string{})

	res, err := f.AddFile(ctx, path, "{not json")
	mustErrCode(t, res, err, ErrInvalidJSON)
	res, err = f.AddFile(ctx, path, `{"name":"api-service"}`)
	mustErrCode(t, res, err, ErrSchemaViolation)
	res, err = f.AddFile(ctx, path, strings.Replace(valid, `"api-service"`, `"wrong-name"`, 1))
	mustErrCode(t, res, err, ErrSchemaViolation)
	if len(f.Touched()) != 0 {
		t.Fatal("gate rejections must not touch")
	}
	res, err = f.AddFile(ctx, path, valid)
	mustOK(t, res, err, StatusApplied)
	// Non-component design.json paths are not gated.
	res, err = f.AddFile(ctx, "other/design.json", "{not json")
	mustOK(t, res, err, StatusApplied)
}

func TestBaseReader_LazyAndErrorPropagation(t *testing.T) {
	reads := 0
	base := func(_ context.Context, path string) ([]byte, bool, error) {
		reads++
		switch path {
		case "a.md":
			return []byte("alpha\r\nbeta\n"), true, nil
		case "boom.md":
			return nil, false, fmt.Errorf("disk on fire")
		default:
			return nil, false, nil
		}
	}
	f := New(base)
	// One read, then cached.
	res, err := f.EditFile(ctx, "a.md", "alpha", "ALPHA")
	mustOK(t, res, err, StatusApplied)
	res, err = f.EditFile(ctx, "a.md", "beta", "BETA")
	mustOK(t, res, err, StatusApplied)
	if reads != 1 {
		t.Fatalf("base reads = %d, want 1 (cache + overlay)", reads)
	}
	// Base content was CRLF; the fold holds LF.
	if c := f.Touched()["a.md"]; c == nil || *c != "ALPHA\nBETA\n" {
		t.Fatalf("content: %v", c)
	}
	// Absent path cached as absent.
	res, err = f.EditFile(ctx, "nope.md", "a", "b")
	mustErrCode(t, res, err, ErrNoSuchFile)
	// Infrastructure error propagates as error, not OpResult.
	if _, err := f.EditFile(ctx, "boom.md", "a", "b"); err == nil || !strings.Contains(err.Error(), "disk on fire") {
		t.Fatalf("want propagated read error, got %v", err)
	}
}

func TestApplyToolCall_DrivingAndIgnoring(t *testing.T) {
	seed := map[string]string{"doc.md": "title: T\nBody\n"}
	f := NewFromSnapshot(seed)
	call := func(tool string, input string) *OpResult {
		t.Helper()
		res, err := f.ApplyToolCall(ctx, StreamPart{Type: "tool-call", ToolName: tool, Input: json.RawMessage(input)})
		if err != nil {
			t.Fatalf("apply error: %v", err)
		}
		return res
	}

	// Non-mutation tools and non-tool-call frames are silently ignored.
	if res, _ := f.ApplyToolCall(ctx, StreamPart{Type: "tool-call", ToolName: "loadSkill", Input: json.RawMessage(`{"names":["x"]}`)}); res != nil {
		t.Fatal("loadSkill must be ignored")
	}
	if res, _ := f.ApplyToolCall(ctx, StreamPart{Type: "tool-result", ToolName: "addFile", Input: json.RawMessage(`{"path":"x.md","content":"y"}`)}); res != nil {
		t.Fatal("tool-result frames must not fold")
	}
	// Malformed inputs are ignored, exactly like the TS applyToolCall guards.
	for _, bad := range []string{
		`null`, `[1,2]`, `{"path":7,"content":"x"}`, `{"path":"x.md"}`,
	} {
		if res, _ := f.ApplyToolCall(ctx, StreamPart{Type: "tool-call", ToolName: "addFile", Input: json.RawMessage(bad)}); res != nil {
			t.Fatalf("malformed input %s must be ignored", bad)
		}
	}

	// The mutation ops drive through with typed values.
	if res := call("addFile", `{"path":"specs/new.md","content":"hello\r\nworld"}`); !res.OK {
		t.Fatalf("%+v", res)
	}
	if res := call("editFile", `{"path":"specs/new.md","oldString":"hello","newString":"HELLO"}`); !res.OK {
		t.Fatalf("%+v", res)
	}
	if res := call("editFile", `{"path":"doc.md","oldString":"title: T","newString":"title: U"}`); !res.OK {
		t.Fatalf("%+v", res)
	}
	if res := call("removeFile", `{"path":"specs/new.md"}`); !res.OK {
		t.Fatalf("%+v", res)
	}

	state := f.FullState(seed)
	want := "title: U\nBody\n"
	if state["doc.md"] != want {
		t.Fatalf("doc.md = %q, want %q", state["doc.md"], want)
	}
	if _, exists := state["specs/new.md"]; exists {
		t.Fatal("specs/new.md should be gone")
	}
}

func TestManifestOfAndIsEmpty(t *testing.T) {
	if _, ok := ManifestOf(StreamPart{Type: "finish"}); ok {
		t.Fatal("non-manifest part accepted")
	}
	m, ok := ManifestOf(StreamPart{Type: "manifest"})
	if !ok || m.Files == nil || !m.IsEmpty() {
		t.Fatalf("empty manifest: %+v ok=%v", m, ok)
	}
	m, ok = ManifestOf(StreamPart{Type: "manifest", Files: map[string]string{"a": "b"}})
	if !ok || m.IsEmpty() {
		t.Fatal("non-empty manifest misread")
	}
	m, ok = ManifestOf(StreamPart{Type: "manifest", Deleted: []string{"x"}})
	if !ok || m.IsEmpty() {
		t.Fatal("deleted-only manifest is not empty")
	}
}

func TestSnapshotFilter(t *testing.T) {
	if !KeepInTurnSnapshot("specs/requirements/requirements.md") ||
		!KeepInTurnSnapshot("a.dsl") ||
		!KeepInTurnSnapshot("specs/design/design.cell") ||
		!KeepInTurnSnapshot("specs/design/components/x/design.json") ||
		!KeepInTurnSnapshot("specs/validation/validation-criteria.json") {
		t.Fatal("keep-filter rejects agent-authored sources")
	}
	if KeepInTurnSnapshot("specs/design/components/x/openapi.yaml") ||
		KeepInTurnSnapshot("design.json.bak") ||
		KeepInTurnSnapshot("x.gen.json") {
		t.Fatal("keep-filter admits derived artifacts")
	}
	if InTurnSnapshot(".hidden/notes.md", nil) || InTurnSnapshot("a/.git/x.md", nil) {
		t.Fatal("dot segments must drop")
	}
	if InTurnSnapshot("bin.md", []byte("a\x00b")) {
		t.Fatal("NUL content must drop")
	}
	got := FilterTurnSnapshot(map[string]string{
		"keep.md": "x", ".drop.md": "y", "code.go": "z", "d/design.json": "j",
	})
	if len(got) != 2 || got["keep.md"] != "x" || got["d/design.json"] != "j" {
		t.Fatalf("filter: %v", got)
	}
}
