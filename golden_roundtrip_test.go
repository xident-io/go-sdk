package xident

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"testing"
)

// The test this SDK needed and did not have.
//
// testdata/tenant_result_v1.golden.json is a copy of the API's own frozen
// fixture. Every SDK's test suite CLAIMED it was a byte-for-byte copy, and for
// a week it was not: the API's was 836 bytes and all four SDK copies were 659,
// missing `risk`, `checks.eu_wallet` and `checks.aml`. Nothing caught it,
// because:
//
//   - encoding/json silently discards unknown fields (which is what makes
//     additive API changes safe, and is therefore not a bug to fix), and
//   - the existing tests parse the fixture and assert individual fields, so a
//     field that neither the fixture nor the struct contained was invisible.
//
// The fixture and the struct agreed with each other perfectly while both
// disagreed with the API. So a test that only reads the fixture can never catch
// this class of drift — it has to compare what went IN against what comes OUT.
//
// Deliberately NOT a byte comparison: Go marshals struct fields in declaration
// order, and the other three SDKs serialize in their own orders, so a
// byte-identical assertion would be a different (and flakier) test in every
// language. Structural comparison of the decoded maps is the property that
// actually matters — no field the API sent may vanish on the way through.
func TestGoldenRoundTripDropsNothing(t *testing.T) {
	raw, err := os.ReadFile("testdata/tenant_result_v1.golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	var result SessionResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal golden into SessionResult: %v", err)
	}

	out, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("re-marshal SessionResult: %v", err)
	}

	var want, got map[string]any
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode round-tripped: %v", err)
	}

	for _, missing := range droppedKeys(want, got, "") {
		t.Errorf("SessionResult drops %s — the API sends it and this SDK discards it. "+
			"Add the field to the struct; do not trim the fixture.", missing)
	}
}

// droppedKeys reports every key path present in want but absent from got,
// recursing into nested objects. It deliberately ignores keys present in got
// but not in want: a field the struct exposes that the fixture omits is the
// normal `omitempty` case, not drift.
func droppedKeys(want, got map[string]any, prefix string) []string {
	var missing []string
	for k, wv := range want {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		gv, ok := got[k]
		if !ok {
			missing = append(missing, path)
			continue
		}
		wo, wIsObj := wv.(map[string]any)
		go_, gIsObj := gv.(map[string]any)
		if wIsObj && gIsObj {
			missing = append(missing, droppedKeys(wo, go_, path)...)
		}
	}
	sort.Strings(missing)
	return missing
}

// The fixture must stay in step with the API's copy. This asserts the field set
// we expect it to carry, so a future sync that silently trims it fails here
// rather than in a customer's integration.
func TestGoldenCarriesEveryDocumentedField(t *testing.T) {
	raw, err := os.ReadFile("testdata/tenant_result_v1.golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var doc struct {
		Checks map[string]any `json:"checks"`
		Risk   *struct {
			Band string `json:"band"`
		} `json:"risk"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode golden: %v", err)
	}

	for _, name := range []string{"liveness", "age", "document", "face_match", "eu_wallet", "aml"} {
		if _, ok := doc.Checks[name]; !ok {
			t.Errorf("golden fixture is missing checks.%s — it is out of sync with the API's "+
				"testdata/tenant_result_v1.golden.json; run `make sdk-golden-sync`", name)
		}
	}
	if doc.Risk == nil {
		t.Error("golden fixture is missing the risk object — out of sync with the API's copy")
	}
}

// Guard the accessor surface: a field the struct holds but no caller can reach
// is only half-shipped. Fails loudly if the parsed values do not survive to the
// exported fields.
func TestNewFieldsAreReadable(t *testing.T) {
	raw, err := os.ReadFile("testdata/tenant_result_v1.golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var result SessionResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if result.Risk == nil {
		t.Fatal("Risk is nil after parsing a fixture that contains a risk object")
	}
	if result.Risk.Band == "" {
		t.Error("Risk.Band is empty after parsing a fixture that sets it")
	}
	// eu_wallet and aml are both performed:false in the fixture, so asserting
	// on Passed would prove nothing. What matters is that the keys were
	// consumed at all, which the round-trip test above establishes; here we
	// only confirm the fields exist and are addressable.
	_ = fmt.Sprint(result.Checks.EUWallet.Performed, result.Checks.AML.Performed)
}
