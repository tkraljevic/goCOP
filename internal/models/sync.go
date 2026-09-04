package models

import (
	"time"

	"github.com/google/uuid"
)

// SyncAction označava tip promjene nad podacima
type SyncAction string

const (
	SyncActionInsert SyncAction = "INSERT"
	SyncActionUpdate SyncAction = "UPDATE"
	SyncActionDelete SyncAction = "DELETE"
)

// SyncLogEntry predstavlja atomsku promjenu u bazi za bez-poslužiteljsku sinkronizaciju
type SyncLogEntry struct {
	ID        uuid.UUID  `json:"id"`         // UUIDv7 osigurava vremenski poredak promjena
	NodeID    string     `json:"node_id"`    // Jedinstveni identifikator čvora/računala
	TableName string     `json:"table_name"` // stations, water_levels, defense_diary
	RecordID  uuid.UUID  `json:"record_id"`  // UUID zapisa
	Action    SyncAction `json:"action"`     // INSERT, UPDATE, DELETE
	DataJSON  string     `json:"data_json"`  // Serializirani JSON sadržaj
	CreatedAt time.Time  `json:"created_at"`
}

// SyncPacket predstavlja paket promjena koji se može prenijeti USB stickom, lokalnom mrežom ili datotekom
type SyncPacket struct {
	Version      string         `json:"version"`        // npr. "gocop/v1"
	PacketID     uuid.UUID      `json:"packet_id"`      // UUIDv7 paketa
	SourceNodeID string         `json:"source_node_id"` // Čvor koji je izvezao paket
	ExportedAt   time.Time      `json:"exported_at"`
	Entries      []SyncLogEntry `json:"entries"`
}

// SyncSummary sažima status sinkronizacije
type SyncSummary struct {
	CurrentNodeID string    `json:"current_node_id"`
	TotalChanges  int       `json:"total_changes"`
	LastSyncedAt  time.Time `json:"last_synced_at"`
}
