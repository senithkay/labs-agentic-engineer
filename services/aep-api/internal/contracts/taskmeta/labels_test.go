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

package taskmeta

import (
	"reflect"
	"testing"
)

func TestLabelBuilders(t *testing.T) {
	if got := ClassLabel(ClassCoding); got != "aep:coding" {
		t.Errorf("ClassLabel(coding) = %q", got)
	}
	if got := ClassLabel(ClassOps); got != "aep:ops" {
		t.Errorf("ClassLabel(ops) = %q", got)
	}
	if got := OriginLabel(OriginSpecPlan); got != "aep:origin/spec-plan" {
		t.Errorf("OriginLabel(spec-plan) = %q", got)
	}
	if got := StatusLabel(StatusReadyForReview); got != "aep:status/ready_for_review" {
		t.Errorf("StatusLabel = %q", got)
	}
	if got := ClassLabel(ClassValidation); got != "aep:validation" {
		t.Errorf("ClassLabel(validation) = %q", got)
	}
	if got := SpecTagLabel("v3"); got != "aep:spec/v3" {
		t.Errorf("SpecTagLabel(v3) = %q", got)
	}
	if got := SpecTagLabel(""); got != "" {
		t.Errorf("SpecTagLabel(\"\") = %q; want empty (no label)", got)
	}
	got := NewTaskLabels(ClassCoding, OriginSpecPlan)
	want := []string{"aep:task", "aep:coding", "aep:origin/spec-plan"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NewTaskLabels = %v; want %v", got, want)
	}
	gotV := NewTaskLabels(ClassValidation, OriginSpecPlan)
	wantV := []string{"aep:task", "aep:validation", "aep:origin/spec-plan"}
	if !reflect.DeepEqual(gotV, wantV) {
		t.Errorf("NewTaskLabels(validation) = %v; want %v", gotV, wantV)
	}
	if !ClassValidation.Valid() {
		t.Error("ClassValidation should be Valid()")
	}
}

func TestClassify(t *testing.T) {
	tests := map[string]LabelKind{
		"aep:task":               KindMarker,
		"aep:coding":             KindClass,
		"aep:ops":                KindClass,
		"aep:validation":         KindClass,
		"aep:origin/incident":    KindOrigin,
		"aep:execute":            KindExecute,
		"aep:hold":               KindHold,
		"aep:status/in_progress": KindStatus,
		"aep:attention":          KindAttention,
		// aep:spec/<tag> is deliberately KindOther so the projection reconciler
		// never strips the build-lineage label.
		"aep:spec/v3": KindOther,
		"bug":         KindOther,
		"aep:unknown": KindOther,
	}
	for label, want := range tests {
		if got := Classify(label); got != want {
			t.Errorf("Classify(%q) = %v; want %v", label, got, want)
		}
	}
}

func TestParseLabels(t *testing.T) {
	p := ParseLabels([]string{
		"aep:task", "aep:coding", "aep:origin/incident",
		"aep:execute", "aep:hold", "aep:attention",
		"aep:status/ready_for_review", "unrelated",
	})
	if !p.IsTask || !p.ClassSet || p.Class != ClassCoding || p.ClassAmbiguous {
		t.Fatalf("class parse wrong: %+v", p)
	}
	if !p.OriginSet || p.Origin != OriginIncident {
		t.Fatalf("origin parse wrong: %+v", p)
	}
	if !p.Execute || !p.Hold || !p.Attention {
		t.Fatalf("command/flag parse wrong: %+v", p)
	}
	if !p.StatusSet || p.Status != StatusReadyForReview {
		t.Fatalf("status parse wrong: %+v", p)
	}
}

func TestParseLabelsEmptyAndAmbiguous(t *testing.T) {
	empty := ParseLabels(nil)
	if empty.IsTask || empty.ClassSet || empty.OriginSet || empty.Execute || empty.Hold || empty.StatusSet {
		t.Fatalf("empty labels should parse to zero facts: %+v", empty)
	}
	amb := ParseLabels([]string{"aep:task", "aep:coding", "aep:ops"})
	if !amb.ClassAmbiguous {
		t.Fatalf("two class labels should set ClassAmbiguous: %+v", amb)
	}
}
