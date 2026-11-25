package portal

import (
	"context"
	"fmt"
	"time"
)

// DatabaseBackup is one daily backup of a database. The table columns are
// those of BackupRow, which is what Rows renders.
type DatabaseBackup struct {
	ID            string     `json:"id"`
	Day           string     `json:"day"`
	FinishedAt    *time.Time `json:"finished_at"`
	SizeBytes     int64      `json:"size_bytes"`
	DatabaseBytes int64      `json:"database_bytes"`
	Status        string     `json:"status"`
	Kind          string     `json:"kind"`
	Extensions    []string   `json:"extensions"`
	PGVersion     string     `json:"pg_version"`
	StartedAt     *time.Time `json:"started_at"`
}

type DatabaseRestore struct {
	ID         string     `json:"id"`
	BackupID   string     `json:"backup_id"`
	Status     string     `json:"status"`
	Error      string     `json:"error"`
	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

// DatabaseBackups is the backup list of a database. Reason says why Available
// is false: "own_server" or "not_enabled".
type DatabaseBackups struct {
	Available bool             `json:"available"`
	Reason    string           `json:"reason,omitempty"`
	Backups   []DatabaseBackup `json:"backups"`
	Restore   *DatabaseRestore `json:"restore,omitempty"`
}

// RemovedDatabase is a database that was deleted but whose backups can still
// be downloaded or restored until AvailableUntil.
type RemovedDatabase struct {
	ID             string           `json:"id"`
	Name           string           `json:"name"`
	Plan           string           `json:"plan"`
	RemovedAt      time.Time        `json:"removed_at"`
	AvailableUntil time.Time        `json:"available_until"`
	Backups        []DatabaseBackup `json:"backups"`
}

type BackupDownload struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

type RestoreRequest struct {
	Name        string `json:"name"`
	Plan        string `json:"plan,omitempty"`
	Environment string `json:"environment,omitempty"`
}

// RestoreResult is the answer to a restore request (HTTP 202): the new
// database, still being restored.
type RestoreResult struct {
	Database Database        `json:"database"`
	Restore  DatabaseRestore `json:"restore"`
}

// BackupRow is one line of the backup table.
type BackupRow struct {
	ID      string `json:"id" table:"ID"`
	Date    string `json:"date" table:"DATE"`
	TakenAt string `json:"taken_at" table:"TAKEN AT"`
	Size    string `json:"size" table:"SIZE"`
	Status  string `json:"status" table:"STATUS"`
}

// Rows is the backups as table rows, in the order the API lists them.
func (b DatabaseBackups) Rows() []BackupRow { return backupRows(b.Backups) }

// Rows is the backups of the removed database as table rows.
func (r RemovedDatabase) Rows() []BackupRow { return backupRows(r.Backups) }

func backupRows(backups []DatabaseBackup) []BackupRow {
	rows := make([]BackupRow, 0, len(backups))
	for _, b := range backups {
		id := b.ID
		if len(id) > 8 {
			id = id[:8]
		}
		taken, size := "-", "-"
		if b.FinishedAt != nil {
			taken = b.FinishedAt.UTC().Format("2006-01-02 15:04")
		}
		if b.Status == "succeeded" {
			size = FormatSize(b.SizeBytes)
		}
		rows = append(rows, BackupRow{ID: id, Date: b.Day, TakenAt: taken, Size: size, Status: backupStatus(b)})
	}
	return rows
}

// backupStatus words the API status for people; a backup taken when the
// database was paused says so.
func backupStatus(b DatabaseBackup) string {
	switch b.Status {
	case "succeeded":
		if b.Kind == "pause" {
			return "completed (on pause)"
		}
		return "completed"
	case "pending":
		return "scheduled"
	}
	return b.Status
}

// FormatSize words a byte count the way the backup table does.
func FormatSize(n int64) string {
	switch {
	case n < 1<<10:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	case n < 1<<30:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	default:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	}
}

func (c *Client) DatabaseBackups(ctx context.Context, org, project, env, name string) (*DatabaseBackups, error) {
	var out DatabaseBackups
	return &out, c.get(ctx, dbPath(org, project, env, name)+"/backups", &out)
}

func (c *Client) DatabaseBackupDownload(ctx context.Context, org, project, env, name, backupID string) (*BackupDownload, error) {
	var out BackupDownload
	return &out, c.get(ctx, dbPath(org, project, env, name)+"/backups/"+esc(backupID)+"/download", &out)
}

func (c *Client) RestoreDatabaseBackup(ctx context.Context, org, project, env, name, backupID string, req RestoreRequest) (*RestoreResult, error) {
	var out RestoreResult
	return &out, c.post(ctx, dbPath(org, project, env, name)+"/backups/"+esc(backupID)+"/restore", req, &out)
}

// RemovedDatabases lists the deleted databases whose backups are still kept.
func (c *Client) RemovedDatabases(ctx context.Context, org, project, env string) ([]RemovedDatabase, error) {
	var out struct {
		RemovedDatabases []RemovedDatabase `json:"removed_databases"`
	}
	err := c.get(ctx, removedPath(org, project, env, ""), &out)
	return out.RemovedDatabases, err
}

func (c *Client) RemovedDatabaseBackupDownload(ctx context.Context, org, project, env, removedID, backupID string) (*BackupDownload, error) {
	var out BackupDownload
	return &out, c.get(ctx, removedPath(org, project, env, removedID)+"/backups/"+esc(backupID)+"/download", &out)
}

func (c *Client) RestoreRemovedDatabaseBackup(ctx context.Context, org, project, env, removedID, backupID string, req RestoreRequest) (*RestoreResult, error) {
	var out RestoreResult
	return &out, c.post(ctx, removedPath(org, project, env, removedID)+"/backups/"+esc(backupID)+"/restore", req, &out)
}

// removedPath is the removed-databases collection, or one entry of it.
func removedPath(org, project, env, id string) string {
	if id == "" {
		return envPath(org, project, env, "/removed-databases")
	}
	return envPath(org, project, env, "/removed-databases/"+esc(id))
}
