package portal

import (
	"context"
	"fmt"
	"time"
)

// DiskCap is a server's disk growth cap in GB; 0 means no cap.
type DiskCap int

func (d DiskCap) String() string {
	if d == 0 {
		return "none"
	}
	return fmt.Sprint(int(d))
}

// ByteCount prints a size in bytes in MB or GB; 0 (not measured yet) is blank.
type ByteCount int64

func (b ByteCount) String() string {
	switch {
	case b <= 0:
		return ""
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	default:
		return fmt.Sprintf("%d MB", (int64(b)+(1<<20)-1)>>20)
	}
}

type DatabaseServer struct {
	ID   string `json:"id" table:"ID"`
	Name string `json:"name" table:"NAME"`
	// Environment is the slug of the environment the server was created for
	// and defaults; empty for a server any environment may use. Protected is
	// a production server, whose instance has deletion protection.
	Environment       string    `json:"environment,omitempty" table:"ENVIRONMENT"`
	EnvironmentID     string    `json:"environment_id,omitempty"`
	Protected         bool      `json:"protected"`
	Provider          string    `json:"provider" table:"PROVIDER"`
	Region            string    `json:"region" table:"REGION"`
	Tier              string    `json:"tier" table:"SIZE"`
	Status            string    `json:"status" table:"STATUS"`
	Stage             string    `json:"stage,omitempty"`
	Host              string    `json:"host,omitempty" table:"HOST"`
	DatabaseCount     int       `json:"database_count" table:"DATABASES"`
	DiskSizeGB        int       `json:"disk_size_gb" table:"DISK (GB)"`
	DiskLimitGB       DiskCap   `json:"disk_limit_gb" table:"CAP (GB)"`
	DiskUsedBytes     ByteCount `json:"disk_used_bytes" table:"USED"`
	DiskWarnedPct     int       `json:"disk_warned_pct"`
	Connections       int       `json:"connections"`
	MaxConnections    int       `json:"max_connections"`
	CloudInstanceName string    `json:"cloud_instance_name,omitempty"`
	ErrorMessage      string    `json:"error_message,omitempty"`
	Scope             string    `json:"scope"`
	CloudAccountID    string    `json:"cloud_account_id"`
	CloudAccount      *struct {
		Label string `json:"label"`
	} `json:"cloud_account,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	DiskCheckedAt      *time.Time `json:"disk_checked_at,omitempty"`
	BrokerRegisteredAt *time.Time `json:"broker_registered_at,omitempty"`
}

type DatabaseServerTier struct {
	Slug    string `json:"slug" table:"SLUG"`
	Label   string `json:"label" table:"SIZE"`
	Price   string `json:"price" table:"GOOGLE PRICE"`
	Default bool   `json:"default" table:"DEFAULT"`
}

type CreateDatabaseServerRequest struct {
	CloudAccountID string `json:"cloud_account_id"`
	Name           string `json:"name"`
	Region         string `json:"region"`
	Tier           string `json:"tier"`
	// Environment is the slug of the environment the server is for.
	Environment string `json:"environment,omitempty"`
	DiskLimitGB *int   `json:"disk_limit_gb,omitempty"`
}

func databaseServersPath(org string) string { return orgPath(org, "/database-servers") }

func (c *Client) DatabaseServers(ctx context.Context, org string) ([]DatabaseServer, error) {
	var out []DatabaseServer
	return out, c.get(ctx, databaseServersPath(org), &out)
}

func (c *Client) CreateDatabaseServer(ctx context.Context, org string, req CreateDatabaseServerRequest) (*DatabaseServer, error) {
	var out DatabaseServer
	return &out, c.post(ctx, databaseServersPath(org), req, &out)
}

// SetDatabaseServerDiskLimit caps how far the server's disk may grow; 0 removes
// the cap.
func (c *Client) SetDatabaseServerDiskLimit(ctx context.Context, org, id string, gb int) error {
	return c.put(ctx, databaseServersPath(org)+"/"+esc(id)+"/disk-limit", map[string]int{"disk_limit_gb": gb}, nil)
}

func (c *Client) RetryDatabaseServer(ctx context.Context, org, id string) error {
	return c.post(ctx, databaseServersPath(org)+"/"+esc(id)+"/retry", nil, nil)
}

func (c *Client) DeleteDatabaseServer(ctx context.Context, org, id string) error {
	return c.delete(ctx, databaseServersPath(org)+"/"+esc(id), nil)
}

// ForgetDatabaseServer drops the server from GoShip without touching the
// cloud; the instance, if it still exists, stays in the customer's project.
func (c *Client) ForgetDatabaseServer(ctx context.Context, org, id string) error {
	return c.delete(ctx, databaseServersPath(org)+"/"+esc(id)+"?force=true", nil)
}

func (c *Client) DatabaseServerTiers(ctx context.Context, org string) ([]DatabaseServerTier, error) {
	var out []DatabaseServerTier
	return out, c.get(ctx, orgPath(org, "/database-server-tiers"), &out)
}
