package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/devalexandre/agno-golang/agno/storage"
	sqlitestore "github.com/devalexandre/agno-golang/agno/storage/sqlite"
)

// sessionDBFileName is the SQLite file backing agno chat session history.
// It lives next to the contextstore data under ~/.ginga-context/.
const sessionDBFileName = "sessions.db"

// defaultSessionDB opens (creating if needed) the shared SQLite store used for
// agno chat session history. It mirrors contextstore.NewDefaultStore's location
// (~/.ginga-context/) so all persisted session data stays in one place.
//
// Callers should treat a returned error as non-fatal: chat still works without
// history, it just loses the in-process conversation memory.
func defaultSessionDB() (storage.DB, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir: %w", err)
	}

	dir := filepath.Join(home, ".ginga-context")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create session dir: %w", err)
	}

	path := filepath.Join(dir, sessionDBFileName)
	db, err := sqlitestore.NewSqliteStorage(sqlitestore.SqliteStorageConfig{
		TableName: "ginga_sessions",
		DBFile:    &path,
	})
	if err != nil {
		return nil, fmt.Errorf("open session db: %w", err)
	}

	if err := db.CreateTables(context.Background()); err != nil {
		return nil, fmt.Errorf("create session tables: %w", err)
	}

	return db, nil
}

// newSessionID mints a fresh chat session identifier. The format matches the
// CLI's session ids so displayed and stored ids stay consistent.
func newSessionID() string {
	return fmt.Sprintf("cli-%d", time.Now().UnixNano())
}
