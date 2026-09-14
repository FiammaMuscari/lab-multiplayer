package session

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/FiammaMuscari/ironsmith-multiplayer-lab/internal/protocol"
	"github.com/FiammaMuscari/ironsmith-multiplayer-lab/internal/store"
)

var (
	ErrUnauthorized  = errors.New("invalid player or resume token")
	ErrConflict      = errors.New("action expected sequence does not match")
	ErrInvalidAction = errors.New("invalid action")
)

type Subscriber struct {
	C     <-chan protocol.ServerMessage
	close func()
}

func (s Subscriber) Close() { s.close() }

type Room struct {
	mu       sync.Mutex
	meta     store.Metadata
	events   []protocol.Event
	byAction map[string]protocol.Event
	subs     map[uint64]chan protocol.ServerMessage
	nextSub  uint64
	store    *store.FileStore
	clock    func() time.Time
}

func NewRoom(fs *store.FileStore, meta store.Metadata, events []protocol.Event) *Room {
	byAction := make(map[string]protocol.Event, len(events))
	for _, event := range events {
		byAction[event.ActionID] = event
	}
	return &Room{meta: meta, events: events, byAction: byAction, subs: make(map[uint64]chan protocol.ServerMessage), store: fs, clock: time.Now}
}

func Create(fs *store.FileStore, name string) (*Room, protocol.Credentials, error) {
	roomID, err := randomID("room", 6)
	if err != nil {
		return nil, protocol.Credentials{}, err
	}
	playerID, err := randomID("player", 6)
	if err != nil {
		return nil, protocol.Credentials{}, err
	}
	token, err := randomHex(32)
	if err != nil {
		return nil, protocol.Credentials{}, err
	}
	meta := store.Metadata{Version: 1, RoomID: roomID, Players: map[string]store.PlayerRecord{
		playerID: {ID: playerID, Name: cleanName(name), TokenHash: store.HashToken(token)},
	}}
	if err = fs.Create(meta); err != nil {
		return nil, protocol.Credentials{}, err
	}
	return NewRoom(fs, meta, nil), protocol.Credentials{RoomID: roomID, PlayerID: playerID, ResumeToken: token}, nil
}

func Load(fs *store.FileStore, roomID string) (*Room, error) {
	meta, events, err := fs.Load(roomID)
	if err != nil {
		return nil, err
	}
	return NewRoom(fs, meta, events), nil
}

func (r *Room) Join(name string) (protocol.Credentials, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.meta.Players) >= 8 {
		return protocol.Credentials{}, fmt.Errorf("room is full")
	}
	playerID, err := randomID("player", 6)
	if err != nil {
		return protocol.Credentials{}, err
	}
	token, err := randomHex(32)
	if err != nil {
		return protocol.Credentials{}, err
	}
	r.meta.Players[playerID] = store.PlayerRecord{ID: playerID, Name: cleanName(name), TokenHash: store.HashToken(token)}
	if err = r.store.SaveMetadata(r.meta); err != nil {
		delete(r.meta.Players, playerID)
		return protocol.Credentials{}, err
	}
	return protocol.Credentials{RoomID: r.meta.RoomID, PlayerID: playerID, ResumeToken: token}, nil
}

func (r *Room) Authenticate(playerID, token string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	player, ok := r.meta.Players[playerID]
	actual := store.HashToken(token)
	return ok && subtle.ConstantTimeCompare([]byte(player.TokenHash), []byte(actual)) == 1
}

func (r *Room) Apply(playerID string, action protocol.Action) (protocol.Event, bool, error) {
	r.mu.Lock()
	if _, ok := r.meta.Players[playerID]; !ok {
		r.mu.Unlock()
		return protocol.Event{}, false, ErrUnauthorized
	}
	if old, ok := r.byAction[action.ActionID]; ok {
		r.mu.Unlock()
		if old.PlayerID != playerID {
			return protocol.Event{}, false, ErrInvalidAction
		}
		return old, true, nil
	}
	if !store.ValidateID(action.ActionID) || len(action.Kind) < 1 || len(action.Kind) > 64 || len(action.Payload) > 256*1024 {
		r.mu.Unlock()
		return protocol.Event{}, false, ErrInvalidAction
	}
	if len(action.Payload) > 0 && !json.Valid(action.Payload) {
		r.mu.Unlock()
		return protocol.Event{}, false, ErrInvalidAction
	}
	current := uint64(len(r.events))
	if action.ExpectedSeq != current {
		r.mu.Unlock()
		return protocol.Event{}, false, ErrConflict
	}
	previous := ""
	if current > 0 {
		previous = r.events[current-1].Hash
	}
	event := protocol.Event{Seq: current + 1, ActionID: action.ActionID, PlayerID: playerID, Kind: action.Kind,
		Payload: append(json.RawMessage(nil), action.Payload...), AcceptedAt: r.clock().UTC(), PrevHash: previous}
	event.Hash = store.HashEvent(event)
	if err := r.store.Append(r.meta.RoomID, event); err != nil {
		r.mu.Unlock()
		return protocol.Event{}, false, err
	}
	r.events = append(r.events, event)
	r.byAction[action.ActionID] = event
	message := protocol.ServerMessage{Type: "event", CurrentSeq: event.Seq, Event: &event}
	for id, sub := range r.subs {
		select {
		case sub <- message:
		default:
			close(sub)
			delete(r.subs, id)
		}
	}
	r.mu.Unlock()
	return event, false, nil
}

func (r *Room) Replay(after uint64) ([]protocol.Event, uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current := uint64(len(r.events))
	if after > current {
		return nil, current, ErrConflict
	}
	result := append([]protocol.Event(nil), r.events[after:]...)
	return result, current, nil
}

func (r *Room) Subscribe(buffer int) Subscriber {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.subscribeLocked(buffer)
}

// SubscribeFrom takes the replay snapshot and installs the live subscriber in
// one critical section. Keeping these operations atomic prevents an event from
// being committed in the gap between catch-up and live delivery.
func (r *Room) SubscribeFrom(after uint64, buffer int) (Subscriber, []protocol.Event, uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current := uint64(len(r.events))
	if after > current {
		return Subscriber{}, nil, current, ErrConflict
	}
	subscriber := r.subscribeLocked(buffer)
	events := append([]protocol.Event(nil), r.events[after:]...)
	return subscriber, events, current, nil
}

func (r *Room) subscribeLocked(buffer int) Subscriber {
	if buffer < 1 {
		buffer = 1
	}
	r.nextSub++
	id := r.nextSub
	ch := make(chan protocol.ServerMessage, buffer)
	r.subs[id] = ch
	var once sync.Once
	return Subscriber{C: ch, close: func() {
		once.Do(func() {
			r.mu.Lock()
			if existing, ok := r.subs[id]; ok {
				delete(r.subs, id)
				close(existing)
			}
			r.mu.Unlock()
		})
	}}
}

func (r *Room) ID() string { return r.meta.RoomID }

func randomID(prefix string, bytes int) (string, error) {
	value, err := randomHex(bytes)
	return prefix + "-" + value, err
}
func randomHex(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func cleanName(name string) string {
	if len(name) > 40 {
		name = name[:40]
	}
	if name == "" {
		return "Player"
	}
	return name
}
