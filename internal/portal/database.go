package portal

import (
	"context"
	"time"
)

// ServicePostgres is the only managed service GoShip offers, which is why the
// CLI exposes it as `goship db` rather than a generic service surface.
const ServicePostgres = "postgresql"

type Database struct {
	ID          string    `json:"id"`
	Name        string    `json:"name" table:"NAME"`
	Team        string    `json:"team"`
	Plan        string    `json:"plan" table:"PLAN"`
	Description string    `json:"description"`
	DBName      string    `json:"db_name" table:"DATABASE"`
	DBUser      string    `json:"db_user"`
	DBPassword  string    `json:"db_password"`
	Status      string    `json:"status" table:"STATUS"`
	Placement   string    `json:"placement" table:"PLACEMENT"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type DatabaseStats struct {
	SizeBytes   int64  `json:"size_bytes"`
	SizeMB      string `json:"size_mb" table:"SIZE (MB)"`
	Connections int    `json:"connections" table:"CONNS"`
	MaxSizeMB   int    `json:"max_size_mb" table:"MAX (MB)"`
	MaxConns    int    `json:"max_connections" table:"MAX CONNS"`
}

type DatabaseUser struct {
	ID         string     `json:"id"`
	Username   string     `json:"username" table:"USERNAME"`
	AccessMode string     `json:"access_mode" table:"ACCESS"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

func (c *Client) Databases(ctx context.Context, org, project string) ([]Database, error) {
	var dbs []Database
	return dbs, c.get(ctx, projectPath(org, project, "/databases"), &dbs)
}

func (c *Client) Database(ctx context.Context, org, project, name string) (*Database, error) {
	var db Database
	return &db, c.get(ctx, dbPath(org, project, name), &db)
}

func (c *Client) DatabaseStats(ctx context.Context, org, project, name string) (*DatabaseStats, error) {
	var stats DatabaseStats
	return &stats, c.get(ctx, dbPath(org, project, name)+"/stats", &stats)
}

func (c *Client) DatabaseUsers(ctx context.Context, org, project, name string) ([]DatabaseUser, error) {
	var users []DatabaseUser
	return users, c.get(ctx, dbPath(org, project, name)+"/users", &users)
}

// NewDatabaseUser is a user just created. Password is the only copy of the
// plaintext password there will ever be.
type NewDatabaseUser struct {
	User     DatabaseUser `json:"user"`
	Password string       `json:"password"`
}

func (c *Client) CreateDatabaseUser(ctx context.Context, org, project, name, username, accessMode string) (*NewDatabaseUser, error) {
	var out NewDatabaseUser
	body := map[string]string{"username": username}
	if accessMode != "" {
		body["access_mode"] = accessMode
	}
	return &out, c.post(ctx, dbPath(org, project, name)+"/users", body, &out)
}

// DatabaseExtension is one extension of a database. Status is "enabled",
// "enabling" (the server is still installing it) or "available" (it can be
// enabled) when it comes from DatabaseExtensions.Rows.
type DatabaseExtension struct {
	Name    string `json:"name" table:"NAME"`
	Version string `json:"version,omitempty" table:"VERSION"`
	Status  string `json:"status,omitempty" table:"STATUS"`
}

type AvailableExtension struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Comment string `json:"comment"`
}

type DatabaseExtensions struct {
	Enabled   []DatabaseExtension  `json:"enabled"`
	Available []AvailableExtension `json:"available"`
	Pending   []string             `json:"pending"`
}

// Rows flattens the three lists into one table: enabled, then enabling,
// then available.
func (e *DatabaseExtensions) Rows() []DatabaseExtension {
	rows := make([]DatabaseExtension, 0, len(e.Enabled)+len(e.Pending)+len(e.Available))
	for _, x := range e.Enabled {
		rows = append(rows, DatabaseExtension{Name: x.Name, Version: x.Version, Status: "enabled"})
	}
	for _, n := range e.Pending {
		rows = append(rows, DatabaseExtension{Name: n, Status: "enabling"})
	}
	for _, a := range e.Available {
		rows = append(rows, DatabaseExtension{Name: a.Name, Version: a.Version, Status: "available"})
	}
	return rows
}

// IsEnabled reports whether name is in the enabled list.
func (e *DatabaseExtensions) IsEnabled(name string) (DatabaseExtension, bool) {
	for _, x := range e.Enabled {
		if x.Name == name {
			return x, true
		}
	}
	return DatabaseExtension{}, false
}

func (c *Client) DatabaseExtensions(ctx context.Context, org, project, name string) (*DatabaseExtensions, error) {
	var exts DatabaseExtensions
	return &exts, c.get(ctx, dbPath(org, project, name)+"/extensions", &exts)
}

// EnableDatabaseExtension returns Status "enabled", or "enabling" when the
// server lets a slow install finish in the background; poll
// DatabaseExtensions until the name shows as enabled.
func (c *Client) EnableDatabaseExtension(ctx context.Context, org, project, name, ext string) (*DatabaseExtension, error) {
	var out DatabaseExtension
	return &out, c.post(ctx, dbPath(org, project, name)+"/extensions", map[string]string{"name": ext}, &out)
}

func (c *Client) DisableDatabaseExtension(ctx context.Context, org, project, name, ext string) error {
	return c.delete(ctx, dbPath(org, project, name)+"/extensions/"+esc(ext), nil)
}

func (c *Client) CreateDatabase(ctx context.Context, org, project, name, plan, description, serverID string) error {
	body := map[string]string{"name": name, "plan": plan, "description": description}
	if serverID != "" {
		body["server_id"] = serverID
	}
	return c.post(ctx, instancesPath(org, project), body, nil)
}

func (c *Client) DeleteDatabase(ctx context.Context, org, project, name string) error {
	return c.delete(ctx, instancesPath(org, project)+"/"+esc(name), nil)
}

func (c *Client) BindDatabase(ctx context.Context, org, project, name, appName string) error {
	return c.post(ctx, instancesPath(org, project)+"/"+esc(name)+"/bind", map[string]string{"app_name": appName}, nil)
}

func (c *Client) UnbindDatabase(ctx context.Context, org, project, name, appName string) error {
	return c.delete(ctx, instancesPath(org, project)+"/"+esc(name)+"/bind/"+esc(appName), nil)
}

func dbPath(org, project, name string) string {
	return projectPath(org, project, "/databases/"+esc(name))
}

func instancesPath(org, project string) string {
	return projectPath(org, project, "/services/"+ServicePostgres+"/instances")
}
