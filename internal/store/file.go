package store

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/FiammaMuscari/ironsmith-multiplayer-lab/internal/protocol"
)

var ErrNotFound = errors.New("room not found")

type PlayerRecord struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	TokenHash string `json:"tokenHash"`
}

type Metadata struct {
	Version int                     `json:"version"`
	RoomID  string                  `json:"roomId"`
	Players map[string]PlayerRecord `json:"players"`
}

type FileStore struct{ root string }

func NewFileStore(root string) *FileStore { return &FileStore{root: root} }

func (s *FileStore) roomDir(roomID string) string { return filepath.Join(s.root, roomID) }

func (s *FileStore) Create(meta Metadata) error {
	dir := s.roomDir(meta.RoomID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dir, "meta.json")); err == nil {
		return fmt.Errorf("room already exists")
	}
	return s.SaveMetadata(meta)
}

func (s *FileStore) SaveMetadata(meta Metadata) error {
	dir := s.roomDir(meta.RoomID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "meta-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(append(data, '\n'))
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	target := filepath.Join(dir, "meta.json")
	if err = os.Rename(name, target); err != nil {
		// Windows cannot always replace an existing file. The journal remains the
		// source of truth; metadata changes are rare and are retried explicitly.
		if removeErr := os.Remove(target); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return err
		}
		if err = os.Rename(name, target); err != nil {
			return err
		}
	}
	return syncDir(dir)
}

func (s *FileStore) Load(roomID string) (Metadata, []protocol.Event, error) {
	var meta Metadata
	data, err := os.ReadFile(filepath.Join(s.roomDir(roomID), "meta.json"))
	if errors.Is(err, os.ErrNotExist) {
		return meta, nil, ErrNotFound
	}
	if err != nil {
		return meta, nil, err
	}
	if err = json.Unmarshal(data, &meta); err != nil {
		return meta, nil, fmt.Errorf("decode metadata: %w", err)
	}
	events, err := s.loadEvents(roomID)
	return meta, events, err
}

func (s *FileStore) loadEvents(roomID string) ([]protocol.Event, error) {
	f, err := os.Open(filepath.Join(s.roomDir(roomID), "events.jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	reader := bufio.NewReader(f)
	var events []protocol.Event
	previous := ""
	for line := 1; ; line++ {
		data, readErr := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(data)) > 0 {
			var event protocol.Event
			if err := json.Unmarshal(data, &event); err != nil {
				return nil, fmt.Errorf("events line %d: %w", line, err)
			}
			if event.Seq != uint64(len(events)+1) || event.PrevHash != previous || event.Hash != HashEvent(event) {
				return nil, fmt.Errorf("events line %d: broken sequence or hash chain", line)
			}
			events = append(events, event)
			previous = event.Hash
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	return events, nil
}

func (s *FileStore) Append(roomID string, event protocol.Event) error {
	dir := s.roomDir(roomID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(f)
	if err = encoder.Encode(event); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func HashEvent(event protocol.Event) string {
	canonical := struct {
		Seq        uint64          `json:"seq"`
		ActionID   string          `json:"actionId"`
		PlayerID   string          `json:"playerId"`
		Kind       string          `json:"kind"`
		Payload    json.RawMessage `json:"payload,omitempty"`
		AcceptedAt string          `json:"acceptedAt"`
		PrevHash   string          `json:"prevHash"`
	}{event.Seq, event.ActionID, event.PlayerID, event.Kind, event.Payload, event.AcceptedAt.UTC().Format("2006-01-02T15:04:05.000000000Z07:00"), event.PrevHash}
	data, _ := json.Marshal(canonical)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func ValidateID(value string) bool {
	if len(value) < 8 || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return !strings.Contains(value, "--")
}

func syncDir(path string) error {
	// Windows does not permit syncing directory handles. File contents are
	// already fsynced above; Unix also flushes the directory entry rename.
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
