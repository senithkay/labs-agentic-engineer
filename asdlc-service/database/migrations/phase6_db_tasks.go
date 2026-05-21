package migrations

import (
	"fmt"
	"log/slog"

	"gorm.io/gorm"
)

// RunPhase6DbTasks adds the `component_type` column to component_tasks.
// This column is set at task-generation time from .asdlc/design.json and
// lets the platform distinguish database provisioning tasks (which follow
// the MCP callback lifecycle: in_progress → testing → deployed) from
// coding tasks (which follow the GitHub PR + build lifecycle).
//
// Idempotent — re-running is a no-op once the column exists.
func RunPhase6DbTasks(db *gorm.DB) error {
	if err := addColumnIfMissing(db, "component_tasks", "component_type",
		`ALTER TABLE component_tasks ADD COLUMN component_type TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("phase6_db_tasks: add component_type: %w", err)
	}
	slog.Info("phase6_db_tasks migration: component_type column ensured")
	return nil
}
