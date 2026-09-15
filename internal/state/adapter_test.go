package state

import (
	"encoding/json"
	"testing"
)

func TestSyntheticAdapterDeterministicApplyAndHash(t *testing.T) {
	adapter := SyntheticStateAdapter{}
	a, err := adapter.Init("seed-1", map[string]any{"players": 2})
	if err != nil { t.Fatal(err) }
	b, err := adapter.Init("seed-1", map[string]any{"players": 2})
	if err != nil { t.Fatal(err) }
	a, err = adapter.Apply(a, json.RawMessage(`{"type":"pass_priority"}`))
	if err != nil { t.Fatal(err) }
	b, err = adapter.Apply(b, json.RawMessage(`{"type":"pass_priority"}`))
	if err != nil { t.Fatal(err) }
	ha, err := adapter.Hash(a); if err != nil { t.Fatal(err) }
	hb, err := adapter.Hash(b); if err != nil { t.Fatal(err) }
	if ha != hb { t.Fatalf("equal transcripts must hash equally: %s != %s", ha, hb) }
}

func TestSyntheticAdapterRejectsInvalidAction(t *testing.T) {
	adapter := SyntheticStateAdapter{}
	state, err := adapter.Init("seed", nil); if err != nil { t.Fatal(err) }
	if _, err = adapter.Apply(state, json.RawMessage(`not-json`)); err == nil { t.Fatal("invalid action should be rejected") }
}
