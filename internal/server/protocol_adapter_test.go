package server

import (
	"encoding/json"
	"testing"

	"github.com/FiammaMuscari/ironsmith-multiplayer-lab/internal/protocol"
	"github.com/FiammaMuscari/ironsmith-multiplayer-lab/internal/session"
	"github.com/FiammaMuscari/ironsmith-multiplayer-lab/internal/store"
)

func TestTrustedCommandAdapterPreservesMetadataAndDetectsPrefixDivergence(t *testing.T) {
	room, creds, err := session.Create(store.NewFileStore(t.TempDir()), "A")
	if err != nil {
		t.Fatal(err)
	}
	command := json.RawMessage(`{"type":"priority_action","action_ref":{"kind":"pass_priority"}}`)
	event, duplicate, err := room.Apply(creds.PlayerID, protocol.Action{
		CommandID: "command-0001", ClientSeq: 99, ExpectedSeq: 0, ActorIndex: creds.PlayerIndex,
		Kind: "trusted_command", Command: command,
	})
	if err != nil || duplicate {
		t.Fatalf("trusted command = %#v duplicate=%v err=%v", event, duplicate, err)
	}
	if event.CommandID != "command-0001" || event.ActionID != "command-0001" || event.Seq != 1 || event.ActorIndex != 0 || event.PrefixHash == "" {
		t.Fatalf("metadata not preserved: %#v", event)
	}
	retry, duplicate, err := room.Apply(creds.PlayerID, protocol.Action{CommandID: "command-0001", ActorIndex: creds.PlayerIndex, Kind: "trusted_command", Command: command})
	if err != nil || !duplicate || retry.Seq != 1 {
		t.Fatalf("duplicate = %#v duplicate=%v err=%v", retry, duplicate, err)
	}
	_, _, err = room.Apply(creds.PlayerID, protocol.Action{CommandID: "command-0002", ExpectedSeq: 1, ActorIndex: creds.PlayerIndex, Kind: "trusted_command", PrefixHash: "bad", Command: command})
	if err != session.ErrPrefixMismatch {
		t.Fatalf("prefix mismatch err = %v", err)
	}
}

func TestBaselineProfileRetainsHostDependency(t *testing.T) {
	room, host, err := session.Create(store.NewFileStore(t.TempDir()), "host")
	if err != nil {
		t.Fatal(err)
	}
	guest, err := room.Join("guest")
	if err != nil {
		t.Fatal(err)
	}
	room.OpenPlayer(host.PlayerID)
	room.OpenPlayer(guest.PlayerID)
	defer room.ClosePlayer(guest.PlayerID)
	defer room.ClosePlayer(host.PlayerID)
	_, _, err = room.ApplyMode(guest.PlayerID, protocol.Action{CommandID: "baseline-1", ExpectedSeq: 0, ActorIndex: guest.PlayerIndex, Kind: "trusted_command", Command: json.RawMessage(`{"type":"test"}`)}, "baseline")
	if err != nil {
		t.Fatalf("guest should act while host is online: %v", err)
	}
	room.ClosePlayer(host.PlayerID)
	_, _, err = room.ApplyMode(guest.PlayerID, protocol.Action{CommandID: "baseline-2", ExpectedSeq: 1, ActorIndex: guest.PlayerIndex, Kind: "trusted_command", Command: json.RawMessage(`{"type":"test"}`)}, "baseline")
	if err != session.ErrHostUnavailable {
		t.Fatalf("baseline host loss err = %v", err)
	}
}
