package protocol

import (
	"encoding/json"
	"time"
)

const Version = 1

type Credentials struct {
	RoomID      string `json:"roomId"`
	PlayerID    string `json:"playerId"`
	PlayerIndex int    `json:"playerIndex"`
	ResumeToken string `json:"resumeToken"`
}

type Action struct {
	ActionID    string          `json:"actionId,omitempty"` // legacy alias
	CommandID   string          `json:"commandId,omitempty"`
	ExpectedSeq uint64          `json:"expectedSeq,omitempty"` // lab cursor
	ClientSeq   uint64          `json:"seq,omitempty"`         // advisory client value
	ActorIndex  int             `json:"actorIndex"`
	Kind        string          `json:"kind,omitempty"`
	Payload     json.RawMessage `json:"payload,omitempty"` // legacy alias
	Command     json.RawMessage `json:"command,omitempty"`
	PrefixHash  string          `json:"prefixHash,omitempty"`
}

type Event struct {
	Seq        uint64          `json:"seq"`
	ActionID   string          `json:"actionId"`
	CommandID  string          `json:"commandId,omitempty"`
	PlayerID   string          `json:"playerId"`
	ActorIndex int             `json:"actorIndex"`
	Kind       string          `json:"kind"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	AcceptedAt time.Time       `json:"acceptedAt"`
	PrevHash   string          `json:"prevHash"`
	Hash       string          `json:"hash"`
	PrefixHash string          `json:"prefixHash,omitempty"`
}

type ClientMessage struct {
	Type        string  `json:"type"`
	Protocol    int     `json:"protocol,omitempty"`
	RoomID      string  `json:"roomId,omitempty"`
	PlayerID    string  `json:"playerId,omitempty"`
	ResumeToken string  `json:"resumeToken,omitempty"`
	AfterSeq    uint64  `json:"afterSeq,omitempty"`
	Mode        string  `json:"mode,omitempty"`
	Action      *Action `json:"action,omitempty"`
}

type ServerMessage struct {
	Type       string  `json:"type"`
	Protocol   int     `json:"protocol,omitempty"`
	RoomID     string  `json:"roomId,omitempty"`
	PlayerID   string  `json:"playerId,omitempty"`
	Mode       string  `json:"mode,omitempty"`
	CurrentSeq uint64  `json:"currentSeq,omitempty"`
	Events     []Event `json:"events,omitempty"`
	Event      *Event  `json:"event,omitempty"`
	Duplicate  bool    `json:"duplicate,omitempty"`
	Divergence bool    `json:"divergence,omitempty"`
	Code       string  `json:"code,omitempty"`
	Message    string  `json:"message,omitempty"`
}
