package behavior

import (
	"fmt"
	"slices"
	"testing"
)

// -schema reads each name's kind, the keys of its table and its args from Entries,
// listed kind by kind and name by name
func TestTheVocabularyListsEachNameWithItsKindKeysAndArgs(t *testing.T) {
	v := testVocabulary()
	v.Emit("EmitEvent", func(string, any) (func(*testCtx), error) { return nil, nil })
	var got []string
	for _, e := range v.Entries() {
		got = append(got, fmt.Sprintf("%s %s %v %v", e.Kind, e.Name, e.Keys, e.Args))
	}
	want := []string{
		"action CancelTelegraph [] []",
		"action EmitEvent [event payload] []",
		"action Materialize [payload] [{spawn string}]",
		"action StoreTarget [] []",
		"action Telegraph [payload] [{color string}]",
		"flag Enraged [] []",
		"flag Shielded [] []",
		"guard And [] [{guards array}]",
		"guard HitWall [] []",
		"guard Not [] [{guard table}]",
		"guard Or [] [{guards array}]",
		"guard Stunned [] []",
		"guard TargetDistance [] [{op op} {cells int}]",
		"probe ResolveTarget [] []",
		"sustained ChargeToStored [] []",
		"sustained Freeze [] []",
		"sustained Home [] []",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("entries\n%q\nwant\n%q", got, want)
	}
}
