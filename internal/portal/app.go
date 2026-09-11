package portal

import (
	"context"
	"time"
)

type App struct {
	ID          string    `json:"id"`
	Name        string    `json:"name" table:"NAME"`
	TsuruName   string    `json:"tsuru_name"`
	OrgID       string    `json:"org_id"`
	Platform    string    `json:"platform" table:"PLATFORM"`
	Pool        string    `json:"pool,omitempty"`
	NodeID      *string   `json:"node_id,omitempty"`
	PlanSlug    string    `json:"plan_slug"`
	PlanName    string    `json:"plan_name" table:"PLAN"`
	Description string    `json:"description,omitempty"`
	Tags        []string  `json:"tags,omitempty"`
	TeamOwner   string    `json:"team_owner,omitempty"`
	Status      string    `json:"status" table:"STATUS"`
	Units       []AppUnit `json:"units"`
	Addresses   []string  `json:"addresses,omitempty" table:"ADDRESS"`
	CNames      []string  `json:"cnames,omitempty"`
	Deploys     uint      `json:"deploys"`
	IP          string    `json:"ip,omitempty"`
	Error       string    `json:"error,omitempty"`
	Paused      bool      `json:"paused,omitempty"`
	IdleWarned  bool      `json:"idle_warned,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type AppUnit struct {
	ID          string `json:"id" table:"ID"`
	Status      string `json:"status" table:"STATUS"`
	ProcessName string `json:"process_name" table:"PROCESS"`
	Address     string `json:"address,omitempty" table:"ADDRESS"`
}

type CreateAppRequest struct {
	Name        string  `json:"name"`
	Platform    string  `json:"platform"`
	Plan        string  `json:"plan,omitempty"`
	Description string  `json:"description,omitempty"`
	NodeID      *string `json:"node_id,omitempty"`
}

func (c *Client) Apps(ctx context.Context, org string) ([]App, error) {
	var apps []App
	return apps, c.get(ctx, "/orgs/"+esc(org)+"/apps", &apps)
}

func (c *Client) App(ctx context.Context, org, name string) (*App, error) {
	var app App
	return &app, c.get(ctx, appPath(org, name), &app)
}

func (c *Client) CreateApp(ctx context.Context, org string, req CreateAppRequest) (*App, error) {
	var app App
	return &app, c.post(ctx, "/orgs/"+esc(org)+"/apps", req, &app)
}

func (c *Client) DeleteApp(ctx context.Context, org, name string) error {
	return c.delete(ctx, appPath(org, name), nil)
}

// Lifecycle runs one of start, stop, restart or wake.
func (c *Client) Lifecycle(ctx context.Context, org, name, action string) error {
	return c.post(ctx, appPath(org, name)+"/"+action, nil, nil)
}

func (c *Client) ScaleApp(ctx context.Context, org, name string, units int) error {
	return c.put(ctx, appPath(org, name)+"/units", map[string]int{"units": units}, nil)
}

func (c *Client) SetAppPlan(ctx context.Context, org, name, plan string) error {
	return c.put(ctx, appPath(org, name)+"/plan", map[string]string{"plan": plan}, nil)
}

type EnvVar struct {
	Name   string `json:"name" table:"NAME"`
	Value  string `json:"value" table:"VALUE"`
	Public bool   `json:"public" table:"PUBLIC"`
}

func (c *Client) Env(ctx context.Context, org, name string) ([]EnvVar, error) {
	var envs []EnvVar
	return envs, c.get(ctx, appPath(org, name)+"/env", &envs)
}

func (c *Client) SetEnv(ctx context.Context, org, name string, envs []EnvVar, noRestart bool) error {
	body := struct {
		Envs      []EnvVar `json:"envs"`
		NoRestart bool     `json:"no_restart"`
	}{envs, noRestart}
	return c.post(ctx, appPath(org, name)+"/env", body, nil)
}

func (c *Client) UnsetEnv(ctx context.Context, org, name string, keys []string, noRestart bool) error {
	body := struct {
		Envs      []string `json:"envs"`
		NoRestart bool     `json:"no_restart"`
	}{keys, noRestart}
	return c.delete(ctx, appPath(org, name)+"/env", body)
}

func appPath(org, name string) string {
	return "/orgs/" + esc(org) + "/apps/" + esc(name)
}
