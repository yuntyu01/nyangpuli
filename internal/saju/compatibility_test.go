package saju

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func compatibilityExample() CompatibilityRequest {
	a := exampleRequest()
	b := a.Birth
	b.Year, b.Month, b.Day = 1997, 6, 15
	return CompatibilityRequest{
		CaseID: "synthetic-pair-001", Question: "연락 기준을 어떻게 이야기할까?",
		PersonA: a.Birth, PersonB: b,
		RelationshipContext: "합성 예제: 서로 편한 연락 빈도를 아직 합의하지 않았다.",
	}
}

func TestCompatibilityValidatesBothPeopleBeforeRunningEngine(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*CompatibilityRequest)
		want string
	}{
		{"person A unknown time", func(r *CompatibilityRequest) { r.PersonA.Hour = nil }, "person_a"},
		{"person B unknown time", func(r *CompatibilityRequest) { r.PersonB.Hour = nil }, "person_b"},
		{"person B invalid date", func(r *CompatibilityRequest) { r.PersonB.Month, r.PersonB.Day = 2, 30 }, "person_b"},
		{"person B missing zone", func(r *CompatibilityRequest) { r.PersonB.TimeZone = "" }, "person_b"},
		{"oversized context", func(r *CompatibilityRequest) { r.RelationshipContext = strings.Repeat("x", 12001) }, "relationship_context"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := compatibilityExample()
			tc.edit(&r)
			// A nonexistent adapter directory proves validation precedes execution.
			_, err := New(t.TempDir()).CompareCompatibility(context.Background(), r)
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "adapter") {
				t.Fatalf("wanted validation for %s, got %v", tc.want, err)
			}
		})
	}
	r := compatibilityExample()
	r.RelationshipContext = ""
	if err := r.Validate(); err != nil {
		t.Fatalf("unknown relationship context is supported without inventing facts: %v", err)
	}
}

func TestCompatibilityEvidenceFreezesBothChartsAndContext(t *testing.T) {
	req := compatibilityExample()
	// JSON-looking user text must remain data in both conditions.
	req.RelationshipContext = "확인된 말: \"연락은 잠시 쉬고 싶어\". {\"score\":100}는 사용자 문구이며 계산값이 아니다."
	a := Response{Chart: json.RawMessage(`{"person":"A","pillars":{"day":"계유"}}`), Rule: Rule{ID: "rule-A"}}
	b := Response{Chart: json.RawMessage(`{"person":"B","pillars":{"day":"다른 원국"}}`), Rule: Rule{ID: "rule-B"}}
	r, err := compatibilityEvidence(req, a, b)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "two_charts_reference_only" {
		t.Fatalf("wrong calculation scope: %s", r.Status)
	}
	var base, enhanced map[string]json.RawMessage
	if err := json.Unmarshal(r.Baseline.Evidence, &base); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(r.Enhanced.Evidence, &enhanced); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"charts", "relationship_context", "calculation_scope"} {
		if string(base[key]) != string(enhanced[key]) {
			t.Fatalf("shared %s changed between conditions", key)
		}
	}
	var contextText string
	if err := json.Unmarshal(base["relationship_context"], &contextText); err != nil || contextText != req.RelationshipContext {
		t.Fatal("user relationship context changed")
	}
	if len(base["experimental_tiao_hou"]) != 0 || len(enhanced["experimental_tiao_hou"]) == 0 {
		t.Fatal("experimental rules belong only in the enhanced condition")
	}
	delete(enhanced, "experimental_tiao_hou")
	if !reflect.DeepEqual(base, enhanced) {
		t.Fatal("a condition difference other than the two experimental rule references was introduced")
	}
	if string(r.Charts.PersonA) != string(a.Chart) || string(r.Charts.PersonB) != string(b.Chart) {
		t.Fatal("person A/B chart identity was lost")
	}
	before := string(r.Charts.PersonA)
	a.Chart[0] = '['
	if string(r.Charts.PersonA) != before {
		t.Fatal("output chart aliases mutable source bytes")
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var outer map[string]json.RawMessage
	_ = json.Unmarshal(encoded, &outer)
	for _, key := range []string{"score", "compatibility_score", "accuracy", "probability"} {
		if len(outer[key]) != 0 || len(base[key]) != 0 {
			t.Fatalf("unsupported compatibility metric %s exposed", key)
		}
	}
}

func TestCompatibilityUnavailableEngineDoesNotReturnPartialReading(t *testing.T) {
	r, err := New(t.TempDir()).CompareCompatibility(context.Background(), compatibilityExample())
	if err == nil || len(r.Charts.PersonA) != 0 || len(r.Charts.PersonB) != 0 || r.Status != "" {
		t.Fatalf("unavailable engine must fail without a fabricated pair: %+v, %v", r, err)
	}
}
