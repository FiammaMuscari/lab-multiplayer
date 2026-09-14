package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FiammaMuscari/ironsmith-multiplayer-lab/internal/protocol"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestWebSocketResumeAndReplay(t *testing.T) {
	httpServer := httptest.NewServer(New(t.TempDir(), nil, nil).Handler())
	defer httpServer.Close()
	host := createCredentials(t, httpServer.URL+"/v1/rooms", "host")
	guest := createCredentials(t, httpServer.URL+"/v1/rooms/"+host.RoomID+"/players", "guest")

	hostConn := connect(t, httpServer.URL, host, 0)
	readType(t, hostConn, "resumed")
	guestConn := connect(t, httpServer.URL, guest, 0)
	readType(t, guestConn, "resumed")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	action := protocol.ClientMessage{Type: "action", Action: &protocol.Action{ActionID: "action-0001", ExpectedSeq: 0, Kind: "pass_priority"}}
	if err := wsjson.Write(ctx, guestConn, action); err != nil {
		t.Fatal(err)
	}
	event := readType(t, guestConn, "event")
	if event.Event == nil || event.Event.Seq != 1 {
		t.Fatalf("bad event: %#v", event)
	}
	readType(t, hostConn, "event")
	guestConn.CloseNow()

	// A resumed socket receives exactly the missing tail, while the creator's
	// socket is irrelevant to the guest's identity and cursor.
	resumed := connect(t, httpServer.URL, guest, 0)
	message := readType(t, resumed, "resumed")
	if message.CurrentSeq != 1 || len(message.Events) != 1 {
		t.Fatalf("bad replay: %#v", message)
	}
	resumed.CloseNow()
	hostConn.CloseNow()
}

func TestServerRestartKeepsSeatAndJournal(t *testing.T) {
	dataDir := t.TempDir()
	firstServer := httptest.NewServer(New(dataDir, nil, nil).Handler())
	credentials := createCredentials(t, firstServer.URL+"/v1/rooms", "player")
	conn := connect(t, firstServer.URL, credentials, 0)
	readType(t, conn, "resumed")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err := wsjson.Write(ctx, conn, protocol.ClientMessage{Type: "action", Action: &protocol.Action{ActionID: "action-0001", ExpectedSeq: 0, Kind: "start_turn"}})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	readType(t, conn, "event")
	conn.CloseNow()
	firstServer.Close()

	secondServer := httptest.NewServer(New(dataDir, nil, nil).Handler())
	defer secondServer.Close()
	resumed := connect(t, secondServer.URL, credentials, 0)
	message := readType(t, resumed, "resumed")
	if message.CurrentSeq != 1 || len(message.Events) != 1 || message.Events[0].ActionID != "action-0001" {
		t.Fatalf("restart lost durable state: %#v", message)
	}
	resumed.CloseNow()
}

func createCredentials(t *testing.T, url, name string) protocol.Credentials {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"name": name})
	response, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create status %d", response.StatusCode)
	}
	var credentials protocol.Credentials
	if err = json.NewDecoder(response.Body).Decode(&credentials); err != nil {
		t.Fatal(err)
	}
	return credentials
}

func connect(t *testing.T, base string, credentials protocol.Credentials, after uint64) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(base, "http") + "/v1/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	hello := protocol.ClientMessage{Type: "resume", Protocol: protocol.Version, RoomID: credentials.RoomID, PlayerID: credentials.PlayerID, ResumeToken: credentials.ResumeToken, AfterSeq: after}
	if err = wsjson.Write(ctx, conn, hello); err != nil {
		t.Fatal(err)
	}
	return conn
}

func readType(t *testing.T, conn *websocket.Conn, expected string) protocol.ServerMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var message protocol.ServerMessage
	if err := wsjson.Read(ctx, conn, &message); err != nil {
		t.Fatal(err)
	}
	if message.Type != expected {
		t.Fatalf("expected %s, got %#v", expected, message)
	}
	return message
}
