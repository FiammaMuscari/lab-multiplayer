package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FiammaMuscari/ironsmith-multiplayer-lab/internal/protocol"
)

func TestLoadRejectsCorruptJournal(t *testing.T) {
	root := t.TempDir()
	fs := NewFileStore(root)
	meta := Metadata{Version: 1, RoomID: "room-12345678", Players: map[string]PlayerRecord{}}
	if err := fs.Create(meta); err != nil {
		t.Fatal(err)
	}
	event := protocol.Event{Seq: 1, ActionID: "action-0001", PlayerID: "player-0001", Kind: "pass", PrevHash: ""}
	event.Hash = HashEvent(event)
	if err := fs.Append(meta.RoomID, event); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, meta.RoomID, "events.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), `"kind":"pass"`, `"kind":"tampered"`, 1))
	if err = os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err = fs.Load(meta.RoomID)
	if err == nil || !strings.Contains(err.Error(), "broken sequence or hash chain") {
		t.Fatalf("expected corruption failure, got %v", err)
	}
}
