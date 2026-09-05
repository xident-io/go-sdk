package xident

import (
	"encoding/json"
	"os"
	"testing"
)

// checks.data_match is OPTIONAL on the wire (sent since 2026-09-05). Two
// fixtures pin both states: the base golden has no data_match key and must
// parse to nil; the data_match golden has it and must parse in full. The
// round-trip helper from golden_roundtrip_test.go proves the struct drops
// nothing the API sends.

func TestDataMatch_AbsentOnTheBaseGolden(t *testing.T) {
	raw, err := os.ReadFile("testdata/tenant_result_v1.golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var result SessionResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Checks.DataMatch != nil {
		t.Fatalf("data_match must be nil when the API omits it, got %+v", result.Checks.DataMatch)
	}
}

func TestDataMatch_ParsedFromItsGolden(t *testing.T) {
	raw, err := os.ReadFile("testdata/tenant_result_v1_data_match.golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var result SessionResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	dm := result.Checks.DataMatch
	if dm == nil {
		t.Fatal("data_match must be parsed when present")
	}
	if !dm.Performed || dm.Passed {
		t.Fatalf("performed=%v passed=%v, want performed=true passed=false", dm.Performed, dm.Passed)
	}
	if dm.Fields.FirstName != DataMatchOutcomeMatch || dm.Fields.DateOfBirth != DataMatchOutcomeMismatch {
		t.Fatalf("fields = %+v", dm.Fields)
	}
	if dm.Fields.LastName != "" || dm.Fields.DocumentNumber != "" || dm.Fields.Nationality != "" {
		t.Fatalf("fields not requested must stay empty: %+v", dm.Fields)
	}

	// Nothing the API sends in this fixture is dropped on the way through.
	out, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	var want, got map[string]any
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	for _, missing := range droppedKeys(want, got, "") {
		t.Errorf("SessionResult drops %s", missing)
	}
}
