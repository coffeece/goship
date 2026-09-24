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

func (c *Client) Databases(ctx context.Context, org string) ([]Database, error) {
	var dbs []Database
	return dbs, c.get(ctx, orgPath(org, "/databases"), &dbs)
}

func (c *Client) Database(ctx context.Context, org, name string) (*Database, error) {
	var db Database
	return &db, c.get(ctx, dbPath(org, name), &db)
}

func (c *Client) DatabaseStats(ctx context.Context, org, name string) (*DatabaseStats, error) {
	var stats DatabaseStats
	return &stats, c.get(ctx, dbPath(org, name)+"/stats", &stats)
}

func (c *Client) DatabaseUsers(ctx context.Context, org, name string) ([]DatabaseUser, error) {
	var users []DatabaseUser
	return users, c.get(ctx, dbPath(org, name)+"/users", &users)
}

// NewDatabaseUser is a user just created. Password is the only copy of the
// plaintext password there will ever be.
type NewDatabaseUser struct {
	User     DatabaseUser `json:"user"`
	Password string       `json:"password"`
}

func (c *Client) CreateDatabaseUser(ctx context.Context, org, name, username, accessMode string) (*NewDatabaseUser, error) {
	var out NewDatabaseUser
	body := map[string]string{"username": username}
	if accessMode != "" {
		body["access_mode"] = accessMode
	}
	return &out, c.post(ctx, dbPath(org, name)+"/users", body, &out)
}

func (c *Client) CreateDatabase(ctx context.Context, org, name, plan, description string) error {
	body := map[string]string{"name": name, "plan": plan, "description": description}
	return c.post(ctx, instancesPath(org), body, nil)
}

func (c *Client) DeleteDatabase(ctx context.Context, org, name string) error {
	return c.delete(ctx, instancesPath(org)+"/"+esc(name), nil)
}

func (c *Client) BindDatabase(ctx context.Context, org, name, appName string) error {
	return c.post(ctx, instancesPath(org)+"/"+esc(name)+"/bind", map[string]string{"app_name": appName}, nil)
}

func (c *Client) UnbindDatabase(ctx context.Context, org, name, appName string) error {
	return c.delete(ctx, instancesPath(org)+"/"+esc(name)+"/bind/"+esc(appName), nil)
}

func dbPath(org, name string) string {
	return orgPath(org, "/databases/"+esc(name))
}

func instancesPath(org string) string {
	return orgPath(org, "/services/"+ServicePostgres+"/instances")
}
