package server

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/FiammaMuscari/ironsmith-multiplayer-lab/internal/protocol"
	"github.com/FiammaMuscari/ironsmith-multiplayer-lab/internal/session"
	"github.com/FiammaMuscari/ironsmith-multiplayer-lab/internal/store"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

//go:embed static/*
var staticFiles embed.FS

const Version = "0.1.0-beta.1"

type Server struct {
	store          *store.FileStore
	log            *slog.Logger
	mu             sync.Mutex
	rooms          map[string]*session.Room
	allowedOrigins []string
}

func New(dataDir string, logger *slog.Logger, allowedOrigins []string) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{store: store.NewFileStore(dataDir), log: logger, rooms: make(map[string]*session.Room), allowedOrigins: allowedOrigins}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": Version})
	})
	mux.HandleFunc("POST /v1/rooms", s.createRoom)
	mux.HandleFunc("POST /v1/rooms/{room}/players", s.joinRoom)
	mux.HandleFunc("POST /v1/rooms/{room}/diagnostics", s.roomDiagnostics)
	mux.HandleFunc("GET /v1/ws", s.websocket)
	mux.Handle("GET /static/", http.FileServer(http.FS(staticFiles)))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, err := staticFiles.ReadFile("static/index.html")
		if err != nil {
			http.Error(w, "client unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	})
	return requestLimits(mux)
}

func (s *Server) createRoom(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		return
	}
	room, credentials, err := session.Create(s.store, body.Name)
	if err != nil {
		problem(w, http.StatusInternalServerError, "create_failed", err.Error())
		return
	}
	s.mu.Lock()
	s.rooms[room.ID()] = room
	s.mu.Unlock()
	writeJSON(w, http.StatusCreated, credentials)
}

func (s *Server) joinRoom(w http.ResponseWriter, r *http.Request) {
	room, err := s.room(r.PathValue("room"))
	if err != nil {
		problem(w, http.StatusNotFound, "room_not_found", "room does not exist")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err = decodeBody(w, r, &body); err != nil {
		return
	}
	credentials, err := room.Join(body.Name)
	if err != nil {
		problem(w, http.StatusConflict, "join_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, credentials)
}

func (s *Server) roomDiagnostics(w http.ResponseWriter, r *http.Request) {
	room, err := s.room(r.PathValue("room"))
	if err != nil {
		problem(w, http.StatusNotFound, "room_not_found", "room does not exist")
		return
	}
	var body struct {
		PlayerID    string `json:"playerId"`
		ResumeToken string `json:"resumeToken"`
	}
	if err = decodeBody(w, r, &body); err != nil {
		return
	}
	if !room.Authenticate(body.PlayerID, body.ResumeToken) {
		problem(w, http.StatusUnauthorized, "unauthorized", "invalid player or resume token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"version": Version, "serverTime": time.Now().UTC(), "room": room.Diagnostics()})
}

func (s *Server) room(id string) (*session.Room, error) {
	if !store.ValidateID(id) {
		return nil, store.ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if room := s.rooms[id]; room != nil {
		return room, nil
	}
	room, err := session.Load(s.store, id)
	if err != nil {
		return nil, err
	}
	s.rooms[id] = room
	return room, nil
}

func (s *Server) websocket(w http.ResponseWriter, r *http.Request) {
	origins := s.allowedOrigins
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: origins, CompressionMode: websocket.CompressionContextTakeover})
	if err != nil {
		s.log.Warn("websocket rejected", "error", err)
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(300 * 1024)
	authCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	var hello protocol.ClientMessage
	err = wsjson.Read(authCtx, conn, &hello)
	cancel()
	if err != nil || hello.Type != "resume" || hello.Protocol != protocol.Version {
		closeProblem(conn, "bad_handshake", "first message must be a compatible resume request")
		return
	}
	room, err := s.room(hello.RoomID)
	if err != nil || !room.Authenticate(hello.PlayerID, hello.ResumeToken) {
		closeProblem(conn, "unauthorized", "invalid room, player, or resume token")
		return
	}
	sub, events, current, err := room.SubscribeFrom(hello.AfterSeq, 64)
	if err != nil {
		closeProblem(conn, "invalid_cursor", "afterSeq is ahead of the room")
		return
	}
	defer sub.Close()
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	writeErr := make(chan error, 1)
	incoming := make(chan protocol.ClientMessage)
	readErr := make(chan error, 1)
	go func() {
		if err := writeMessage(ctx, conn, protocol.ServerMessage{Type: "resumed", Protocol: protocol.Version, RoomID: room.ID(), PlayerID: hello.PlayerID, CurrentSeq: current, Events: events}); err != nil {
			writeErr <- err
			return
		}
		for message := range sub.C {
			if err := writeMessage(ctx, conn, message); err != nil {
				writeErr <- err
				return
			}
		}
		writeErr <- errors.New("slow consumer disconnected")
	}()
	go func() {
		for {
			var message protocol.ClientMessage
			if err := wsjson.Read(ctx, conn, &message); err != nil {
				readErr <- err
				return
			}
			select {
			case incoming <- message:
			case <-ctx.Done():
				return
			}
		}
	}()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case err := <-writeErr:
			s.log.Debug("websocket writer stopped", "room", room.ID(), "player", hello.PlayerID, "error", err)
			return
		case <-readErr:
			return
		case <-ping.C:
			pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
			err := conn.Ping(pingCtx)
			pingCancel()
			if err != nil {
				return
			}
		case message := <-incoming:
			if message.Type != "action" || message.Action == nil {
				sendProblem(ctx, conn, "bad_message", "expected an action message")
				continue
			}
			event, duplicate, applyErr := room.Apply(hello.PlayerID, *message.Action)
			if applyErr != nil {
				code := "action_failed"
				if errors.Is(applyErr, session.ErrConflict) {
					code = "sequence_conflict"
				}
				if errors.Is(applyErr, session.ErrInvalidAction) {
					code = "invalid_action"
				}
				sendProblem(ctx, conn, code, applyErr.Error())
				continue
			}
			if duplicate {
				_ = writeMessage(ctx, conn, protocol.ServerMessage{Type: "action_ack", CurrentSeq: event.Seq, Event: &event, Duplicate: true})
			}
		}
	}
}

func writeMessage(parent context.Context, conn *websocket.Conn, value protocol.ServerMessage) error {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	return wsjson.Write(ctx, conn, value)
}
func sendProblem(ctx context.Context, conn *websocket.Conn, code, message string) {
	_ = writeMessage(ctx, conn, protocol.ServerMessage{Type: "error", Code: code, Message: message})
}
func closeProblem(conn *websocket.Conn, code, message string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = wsjson.Write(ctx, conn, protocol.ServerMessage{Type: "error", Code: code, Message: message})
	_ = conn.Close(websocket.StatusPolicyViolation, code)
}

func decodeBody(w http.ResponseWriter, r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		problem(w, http.StatusBadRequest, "bad_json", "invalid JSON body")
		return err
	}
	return nil
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func problem(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"code": code, "message": message})
}
func requestLimits(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
		}
		next.ServeHTTP(w, r)
	})
}

func ParseOrigins(value string) []string {
	var out []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
func (s *Server) String() string {
	return fmt.Sprintf("multiplayer server with %d loaded rooms", len(s.rooms))
}
