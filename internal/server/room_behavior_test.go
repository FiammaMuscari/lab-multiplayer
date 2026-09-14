package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/FiammaMuscari/ironsmith-multiplayer-lab/internal/protocol"
	"github.com/FiammaMuscari/ironsmith-multiplayer-lab/internal/session"
	"github.com/FiammaMuscari/ironsmith-multiplayer-lab/internal/store"
)

func TestReconnectIdempotencyAndNoHostDependency(t *testing.T) {
	fs := store.NewFileStore(t.TempDir())
	room, host, err := session.Create(fs, "host")
	if err != nil {
		t.Fatal(err)
	}
	guest, err := room.Join("guest")
	if err != nil {
		t.Fatal(err)
	}

	first := protocol.Action{ActionID: "action-0001", ExpectedSeq: 0, ActorIndex: host.PlayerIndex, Kind: "trusted_command", Payload: json.RawMessage(`{"type":"priority_action","action_ref":{"kind":"pass_priority"}}`)}
	event, duplicate, err := room.Apply(host.PlayerID, first)
	if err != nil || duplicate || event.Seq != 1 {
		t.Fatalf("first apply = %#v, %v, %v", event, duplicate, err)
	}

	// The creator/old browser host can be gone. The guest still advances the
	// durable sequence because authority belongs to the room, not a socket.
	second := protocol.Action{ActionID: "action-0002", ExpectedSeq: 1, ActorIndex: guest.PlayerIndex, Kind: "trusted_command", Payload: json.RawMessage(`{"type":"priority_action","action_ref":{"kind":"pass_priority"}}`)}
	event, duplicate, err = room.Apply(guest.PlayerID, second)
	if err != nil || duplicate || event.Seq != 2 {
		t.Fatalf("guest apply = %#v, %v, %v", event, duplicate, err)
	}

	retried, duplicate, err := room.Apply(guest.PlayerID, second)
	if err != nil || !duplicate || retried.Hash != event.Hash {
		t.Fatalf("retry = %#v, %v, %v", retried, duplicate, err)
	}

	tail, current, err := room.Replay(1)
	if err != nil || current != 2 || len(tail) != 1 || tail[0].ActionID != "action-0002" {
		t.Fatalf("replay = %#v, %d, %v", tail, current, err)
	}
}

func TestCreatorDisconnectProfilesRemainComparable(t *testing.T) {
	for _, mode := range []string{"baseline", "lab"} {
		t.Run(mode, func(t *testing.T) {
			fs := store.NewFileStore(t.TempDir())
			room, host, err := session.Create(fs, "host")
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

			apply := func(player protocol.Credentials, id string, expected uint64) (protocol.Event, error) {
				event, _, applyErr := room.ApplyMode(player.PlayerID, protocol.Action{
					CommandID: id, ExpectedSeq: expected, ActorIndex: player.PlayerIndex,
					Kind: "trusted_command", Command: json.RawMessage(`{"type":"priority_action"}`),
				}, mode)
				return event, applyErr
			}

			if _, err = apply(host, mode+"-1", 0); err != nil {
				t.Fatal(err)
			}
			if _, err = apply(guest, mode+"-2", 1); err != nil {
				t.Fatal(err)
			}
			room.ClosePlayer(host.PlayerID)
			third, err := apply(guest, mode+"-3", 2)
			if mode == "baseline" {
				if !errors.Is(err, session.ErrHostUnavailable) {
					t.Fatalf("baseline after creator disconnect = %v", err)
				}
				if _, current, replayErr := room.Replay(2); replayErr != nil || current != 2 {
					t.Fatalf("baseline sequence advanced: current=%d err=%v", current, replayErr)
				}
				return
			}
			if err != nil || third.Seq != 3 {
				t.Fatalf("lab guest progress = %#v err=%v", third, err)
			}

			room.OpenPlayer(host.PlayerID)
			tail, current, replayErr := room.Replay(2)
			if replayErr != nil || current != 3 || len(tail) != 1 || tail[0].Seq != 3 || tail[0].PrefixHash != third.PrefixHash {
				t.Fatalf("lab creator recovery = tail=%#v current=%d err=%v", tail, current, replayErr)
			}
		})
	}
}

func TestRejectsStaleSequenceAndSurvivesReload(t *testing.T) {
	fs := store.NewFileStore(t.TempDir())
	room, credentials, err := session.Create(fs, "player")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = room.Apply(credentials.PlayerID, protocol.Action{ActionID: "action-0001", ExpectedSeq: 0, ActorIndex: credentials.PlayerIndex, Kind: "trusted_command", Payload: json.RawMessage(`{"type":"draw"}`)})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = room.Apply(credentials.PlayerID, protocol.Action{ActionID: "action-0002", ExpectedSeq: 0, ActorIndex: credentials.PlayerIndex, Kind: "trusted_command", Payload: json.RawMessage(`{"type":"draw"}`)})
	if !errors.Is(err, session.ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}

	reloaded, err := session.Load(fs, credentials.RoomID)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.Authenticate(credentials.PlayerID, credentials.ResumeToken) {
		t.Fatal("resume token did not survive reload")
	}
	events, current, err := reloaded.Replay(0)
	if err != nil || current != 1 || len(events) != 1 {
		t.Fatalf("reloaded replay = %#v, %d, %v", events, current, err)
	}
}

func TestSlowSubscriberDoesNotBlockRoom(t *testing.T) {
	fs := store.NewFileStore(t.TempDir())
	room, credentials, err := session.Create(fs, "player")
	if err != nil {
		t.Fatal(err)
	}
	slow := room.Subscribe(1)
	defer slow.Close()
	for index := 0; index < 3; index++ {
		_, _, err = room.Apply(credentials.PlayerID, protocol.Action{ActionID: fmt.Sprintf("action-%04d", index+1), ExpectedSeq: uint64(index), ActorIndex: credentials.PlayerIndex, Kind: "trusted_command", Payload: json.RawMessage(`{"type":"pass_priority"}`)})
		if err != nil {
			t.Fatal(err)
		}
	}
	_, ok := <-slow.C
	if !ok {
		t.Fatal("first buffered event should remain readable")
	}
	_, ok = <-slow.C
	if ok {
		t.Fatal("slow subscriber should be closed instead of blocking the room")
	}
}

func TestSubscribeFromClosesReplayToLiveGap(t *testing.T) {
	fs := store.NewFileStore(t.TempDir())
	room, credentials, err := session.Create(fs, "player")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = room.Apply(credentials.PlayerID, protocol.Action{ActionID: "action-0001", ExpectedSeq: 0, ActorIndex: credentials.PlayerIndex, Kind: "trusted_command", Payload: json.RawMessage(`{"type":"pass_priority"}`)})
	if err != nil {
		t.Fatal(err)
	}
	sub, replay, current, err := room.SubscribeFrom(0, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	if current != 1 || len(replay) != 1 {
		t.Fatalf("unexpected atomic replay: current=%d events=%d", current, len(replay))
	}
	_, _, err = room.Apply(credentials.PlayerID, protocol.Action{ActionID: "action-0002", ExpectedSeq: 1, ActorIndex: credentials.PlayerIndex, Kind: "trusted_command", Payload: json.RawMessage(`{"type":"pass_priority"}`)})
	if err != nil {
		t.Fatal(err)
	}
	live := <-sub.C
	if live.Event == nil || live.Event.Seq != 2 {
		t.Fatalf("expected next live event, got %#v", live)
	}
}

func TestConcurrentActionsChooseOneSequenceOwner(t *testing.T) {
	fs := store.NewFileStore(t.TempDir())
	room, first, err := session.Create(fs, "first")
	if err != nil {
		t.Fatal(err)
	}
	players := []protocol.Credentials{first}
	for index := 1; index < 8; index++ {
		joined, joinErr := room.Join(fmt.Sprintf("player-%d", index))
		if joinErr != nil {
			t.Fatal(joinErr)
		}
		players = append(players, joined)
	}

	start := make(chan struct{})
	results := make(chan error, len(players))
	var group sync.WaitGroup
	for index, player := range players {
		group.Add(1)
		go func(index int, player protocol.Credentials) {
			defer group.Done()
			<-start
			_, _, applyErr := room.Apply(player.PlayerID, protocol.Action{ActionID: fmt.Sprintf("action-%04d", index+1), ExpectedSeq: 0, ActorIndex: player.PlayerIndex, Kind: "trusted_command", Payload: json.RawMessage(`{"type":"pass_priority"}`)})
			results <- applyErr
		}(index, player)
	}
	close(start)
	group.Wait()
	close(results)

	accepted, conflicts := 0, 0
	for result := range results {
		switch {
		case result == nil:
			accepted++
		case errors.Is(result, session.ErrConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent result: %v", result)
		}
	}
	if accepted != 1 || conflicts != len(players)-1 {
		t.Fatalf("accepted=%d conflicts=%d", accepted, conflicts)
	}
	events, current, err := room.Replay(0)
	if err != nil || current != 1 || len(events) != 1 {
		t.Fatalf("concurrent journal diverged: current=%d events=%d err=%v", current, len(events), err)
	}
}
