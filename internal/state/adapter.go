// Package state defines the narrow state boundary used by experimental
// clients. The multiplayer server remains unaware of game rules.
package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// StateAdapter applies already-accepted actions to a deterministic state.
// Implementations may be backed by a game runtime; the server does not call it
// to decide ordering or authority.
type StateAdapter interface {
	Init(seed string, config any) (any, error)
	Apply(state any, action json.RawMessage) (any, error)
	Hash(state any) (string, error)
}

// SyntheticStateAdapter is the dependency-free adapter used by the lab.
// It records opaque accepted actions without implementing game rules.
type SyntheticStateAdapter struct{}

type syntheticState struct {
	Seed    string            `json:"seed"`
	Config  json.RawMessage   `json:"config,omitempty"`
	Actions []json.RawMessage `json:"actions"`
}

func (SyntheticStateAdapter) Init(seed string, config any) (any, error) {
	encoded, err := json.Marshal(config)
	if err != nil { return nil, fmt.Errorf("encode state config: %w", err) }
	return syntheticState{Seed: seed, Config: encoded, Actions: []json.RawMessage{}}, nil
}

func (SyntheticStateAdapter) Apply(value any, action json.RawMessage) (any, error) {
	current, ok := value.(syntheticState)
	if !ok { return nil, fmt.Errorf("invalid synthetic state") }
	if !json.Valid(action) { return nil, fmt.Errorf("invalid action JSON") }
	current.Actions = append(append([]json.RawMessage(nil), current.Actions...), append(json.RawMessage(nil), action...))
	return current, nil
}

func (SyntheticStateAdapter) Hash(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil { return "", fmt.Errorf("encode state: %w", err) }
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
