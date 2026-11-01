package portal

import "context"

type Volume struct {
	Name        string       `json:"name" table:"NAME"`
	Pool        string       `json:"pool"`
	Plan        string       `json:"plan" table:"PLAN"`
	TeamOwner   string       `json:"team_owner"`
	CapacityGiB int          `json:"capacity_gib" table:"GiB"`
	Binds       []VolumeBind `json:"binds"`
	// Cluster is where the volume lives.
	Cluster ClusterRef `json:"cluster" table:"CLUSTER"`
}

type VolumeBind struct {
	App        string `json:"app" table:"APP"`
	MountPoint string `json:"mount_point" table:"MOUNT"`
	ReadOnly   bool   `json:"read_only" table:"READ-ONLY"`
}

func (c *Client) Volumes(ctx context.Context, org, project, env string) ([]Volume, error) {
	var volumes []Volume
	return volumes, c.get(ctx, volumesPath(org, project, env), &volumes)
}

func (c *Client) Volume(ctx context.Context, org, project, env, name string) (*Volume, error) {
	var v Volume
	return &v, c.get(ctx, volumesPath(org, project, env)+"/"+esc(name), &v)
}

func (c *Client) CreateVolume(ctx context.Context, org, project, env, name, plan string, capacityGiB int) (*Volume, error) {
	body := map[string]any{"name": name, "plan": plan, "capacity_gib": capacityGiB}
	var v Volume
	return &v, c.post(ctx, volumesPath(org, project, env), body, &v)
}

func (c *Client) BindVolume(ctx context.Context, org, project, env, name, app, mountPoint string, readOnly bool) error {
	body := map[string]any{"app": app, "mount_point": mountPoint, "read_only": readOnly}
	return c.post(ctx, volumesPath(org, project, env)+"/"+esc(name)+"/bind", body, nil)
}

func (c *Client) UnbindVolume(ctx context.Context, org, project, env, name, app, mountPoint string) error {
	body := map[string]string{"app": app, "mount_point": mountPoint}
	return c.post(ctx, volumesPath(org, project, env)+"/"+esc(name)+"/unbind", body, nil)
}

func (c *Client) DeleteVolume(ctx context.Context, org, project, env, name string) error {
	return c.delete(ctx, volumesPath(org, project, env)+"/"+esc(name), nil)
}

func volumesPath(org, project, env string) string { return envPath(org, project, env, "/volumes") }
