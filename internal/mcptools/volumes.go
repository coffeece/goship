package mcptools

import (
	"context"

	"github.com/coffeece/goship/internal/deploy"
	"github.com/coffeece/goship/internal/portal"
)

type VolumeList struct {
	Volumes []portal.Volume `json:"volumes"`
}

type VolumeArg struct {
	OrgArg
	Volume string `json:"volume" jsonschema:"volume name"`
}

type CreateVolumeArgs struct {
	OrgArg
	Name        string `json:"name"`
	Node        string `json:"node" jsonschema:"node the disk lives on (volumes exist only on your own nodes)"`
	Plan        string `json:"plan" jsonschema:"volume plan slug (goship_list_plans with kind=volume and the node)"`
	CapacityGiB int    `json:"capacity_gib" jsonschema:"size in GiB"`
}

type VolumeBindArgs struct {
	VolumeArg
	App        string `json:"app"`
	MountPoint string `json:"mount_point" jsonschema:"absolute path inside the app, e.g. /data"`
	ReadOnly   bool   `json:"read_only,omitempty"`
}

func (r *registry) volumes() {
	tool(r, "goship_list_volumes", "List persistent disks on the organization's nodes and what they are mounted in.", read,
		func(ctx context.Context, c *portal.Client, org string, _ OrgArg) (VolumeList, error) {
			v, err := c.Volumes(ctx, org)
			return VolumeList{Volumes: v}, err
		})
	tool(r, "goship_get_volume", "Show one volume.", read,
		func(ctx context.Context, c *portal.Client, org string, in VolumeArg) (*portal.Volume, error) {
			return c.Volume(ctx, org, in.Volume)
		})
	tool(r, "goship_create_volume", "Create a persistent disk on one of the organization's nodes.", write,
		func(ctx context.Context, c *portal.Client, org string, in CreateVolumeArgs) (*portal.Volume, error) {
			id, err := deploy.ResolveNode(ctx, c, org, in.Node)
			if err != nil {
				return nil, err
			}
			return c.CreateVolume(ctx, org, in.Name, id, in.Plan, in.CapacityGiB)
		})
	tool(r, "goship_bind_volume", "Mount a volume in an app at a path. The app restarts.", write,
		func(ctx context.Context, c *portal.Client, org string, in VolumeBindArgs) (Message, error) {
			if err := c.BindVolume(ctx, org, in.Volume, in.App, in.MountPoint, in.ReadOnly); err != nil {
				return Message{}, err
			}
			return msgf("Volume %s mounted in %s at %s.", in.Volume, in.App, in.MountPoint)
		})
	tool(r, "goship_unbind_volume", "Unmount a volume from an app. The data stays on the disk.", write,
		func(ctx context.Context, c *portal.Client, org string, in VolumeBindArgs) (Message, error) {
			if err := c.UnbindVolume(ctx, org, in.Volume, in.App, in.MountPoint); err != nil {
				return Message{}, err
			}
			return msgf("Volume %s unmounted from %s.", in.Volume, in.App)
		})
	tool(r, "goship_delete_volume", "Delete a volume and the data on it.", destructive,
		func(ctx context.Context, c *portal.Client, org string, in VolumeArg) (Message, error) {
			if err := c.DeleteVolume(ctx, org, in.Volume); err != nil {
				return Message{}, err
			}
			return msgf("Volume %s deleted.", in.Volume)
		})
}
