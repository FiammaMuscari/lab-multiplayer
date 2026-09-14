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
	ActionID    string          `json:"actionId"`
	ExpectedSeq uint64          `json:"expectedSeq"`
	ActorIndex  int             `json:"actorIndex"`
	Kind        string          `json:"kind"`
	Payload     json.RawMessage `json:"payload,omitempty"`
}

type Event struct {
	Seq        uint64          `json:"seq"`
	ActionID   string          `json:"actionId"`
	PlayerID   string          `json:"playerId"`
	ActorIndex int             `json:"actorIndex"`
	Kind       string          `json:"kind"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	AcceptedAt time.Time       `json:"acceptedAt"`
	PrevHash   string          `json:"prevHash"`
	Hash       string          `json:"hash"`
}

type ClientMessage struct {
	Type        string  `json:"type"`
	Protocol    int     `json:"protocol,omitempty"`
	RoomID      string  `json:"roomId,omitempty"`
	PlayerID    string  `json:"playerId,omitempty"`
	ResumeToken string  `json:"resumeToken,omitempty"`
	AfterSeq    uint64  `json:"afterSeq,omitempty"`
	Action      *Action `json:"action,omitempty"`
}

type ServerMessage struct {
	Type       string  `json:"type"`
	Protocol   int     `json:"protocol,omitempty"`
	RoomID     string  `json:"roomId,omitempty"`
	PlayerID   string  `json:"playerId,omitempty"`
	CurrentSeq uint64  `json:"currentSeq,omitempty"`
	Events     []Event `json:"events,omitempty"`
	Event      *Event  `json:"event,omitempty"`
	Duplicate  bool    `json:"duplicate,omitempty"`
	Code       string  `json:"code,omitempty"`
	Message    string  `json:"message,omitempty"`
}
