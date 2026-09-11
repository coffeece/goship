package portal

import "context"

type Volume struct {
	Name        string       `json:"name" table:"NAME"`
	Pool        string       `json:"pool"`
	Plan        string       `json:"plan" table:"PLAN"`
	TeamOwner   string       `json:"team_owner"`
	CapacityGiB int          `json:"capacity_gib" table:"GiB"`
	Binds       []VolumeBind `json:"binds"`
	NodeID      string       `json:"node_id,omitempty"`
	NodeName    string       `json:"node_name,omitempty" table:"NODE"`
}

type VolumeBind struct {
	App        string `json:"app" table:"APP"`
	MountPoint string `json:"mount_point" table:"MOUNT"`
	ReadOnly   bool   `json:"read_only" table:"READ-ONLY"`
}

func (c *Client) Volumes(ctx context.Context, org string) ([]Volume, error) {
	var volumes []Volume
	return volumes, c.get(ctx, volumesPath(org), &volumes)
}

func (c *Client) Volume(ctx context.Context, org, name string) (*Volume, error) {
	var v Volume
	return &v, c.get(ctx, volumesPath(org)+"/"+esc(name), &v)
}

func (c *Client) CreateVolume(ctx context.Context, org, name, nodeID, plan string, capacityGiB int) (*Volume, error) {
	body := map[string]any{"name": name, "node_id": nodeID, "plan": plan, "capacity_gib": capacityGiB}
	var v Volume
	return &v, c.post(ctx, volumesPath(org), body, &v)
}

func (c *Client) BindVolume(ctx context.Context, org, name, app, mountPoint string, readOnly bool) error {
	body := map[string]any{"app": app, "mount_point": mountPoint, "read_only": readOnly}
	return c.post(ctx, volumesPath(org)+"/"+esc(name)+"/bind", body, nil)
}

func (c *Client) UnbindVolume(ctx context.Context, org, name, app, mountPoint string) error {
	body := map[string]string{"app": app, "mount_point": mountPoint}
	return c.post(ctx, volumesPath(org)+"/"+esc(name)+"/unbind", body, nil)
}

func (c *Client) DeleteVolume(ctx context.Context, org, name string) error {
	return c.delete(ctx, volumesPath(org)+"/"+esc(name), nil)
}

func volumesPath(org string) string { return "/orgs/" + esc(org) + "/volumes" }
