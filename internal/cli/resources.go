package cli

import (
	"fmt"
	"os"

	"github.com/coffeece/goship/internal/deploy"
	"github.com/coffeece/goship/internal/portal"
	"github.com/coffeece/goship/internal/render"
	"github.com/spf13/cobra"
)

// newDomainsCmd is the plural lister, matching `apps`.
func newDomainsCmd(app *App) *cobra.Command {
	return newOrgListCmd(app, "domains", "List registered domains in the current organization, or --all of them", (*portal.Client).OrgDomains)
}

func newDomainCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "domain",
		Short: "Manage custom domains",
	}

	add := &cobra.Command{
		Use:   "add <domain> --app <app>",
		Short: "Point a domain at an app",
		Long: "Attach a custom domain to an app, then create a CNAME from it to the\n" +
			"app's default address. TLS is issued automatically once DNS resolves.",
		Args: cobra.ExactArgs(1),
	}
	var addApp string
	add.Flags().StringVarP(&addApp, "app", "a", "", "app to attach the domain to (required)")
	_ = add.MarkFlagRequired("app")
	add.RunE = targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
		if err := app.Portal().AddAppDomain(cmd.Context(), t.Org, t.Project, addApp, args[0]); err != nil {
			return err
		}
		return app.Renderer().Message("Domain %s attached to %s.", args[0], addApp)
	})

	remove := &cobra.Command{
		Use:     "rm <domain> --app <app>",
		Aliases: []string{"remove"},
		Short:   "Detach a domain from an app",
		Args:    cobra.ExactArgs(1),
	}
	var rmApp string
	remove.Flags().StringVarP(&rmApp, "app", "a", "", "app to detach the domain from (required)")
	_ = remove.MarkFlagRequired("app")
	remove.RunE = targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
		if err := app.Portal().RemoveAppDomain(cmd.Context(), t.Org, t.Project, rmApp, args[0]); err != nil {
			return err
		}
		return app.Renderer().Message("Domain %s detached from %s.", args[0], rmApp)
	})

	list := deprecatedList(newDomainsCmd(app))

	register := &cobra.Command{
		Use:   "register <domain>",
		Short: "Register a domain the organization owns",
		Args:  cobra.ExactArgs(1),
		RunE: orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
			created, err := app.Portal().RegisterOrgDomain(cmd.Context(), org, args[0])
			if err != nil {
				return err
			}
			return app.Renderer().Render(created)
		}),
	}

	verify := &cobra.Command{
		Use:   "verify <domain-id>",
		Short: "Check the DNS proof for a registered domain",
		Args:  cobra.ExactArgs(1),
		RunE: orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
			if err := app.Portal().VerifyOrgDomain(cmd.Context(), org, args[0]); err != nil {
				return err
			}
			return app.Renderer().Message("Domain %s verified.", args[0])
		}),
	}

	cmd.AddCommand(add, remove, list, register, verify)
	return cmd
}

// newVolumesCmd is the plural lister, matching `apps`.
func newVolumesCmd(app *App) *cobra.Command {
	return newTargetListCmd(app, "volumes", "List volumes in the current project, or --all of them", (*portal.Client).Volumes)
}

func newVolumeCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "volume",
		Short: "Manage persistent disks on your own nodes",
	}

	create := &cobra.Command{
		Use:   "create <name> --node <node>",
		Short: "Create a volume on one of your nodes",
		Args:  cobra.ExactArgs(1),
	}
	var node, plan string
	var size int
	create.Flags().StringVar(&node, "node", "", "node name or id (required)")
	create.Flags().StringVar(&plan, "plan", "byon-local", "byon-local or do-block-storage")
	create.Flags().IntVar(&size, "size", 5, "capacity in GiB")
	_ = create.MarkFlagRequired("node")
	create.RunE = targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
		nodeID, err := deploy.ResolveNode(cmd.Context(), app.Portal(), t, node)
		if err != nil {
			return err
		}
		v, err := app.Portal().CreateVolume(cmd.Context(), t.Org, t.Project, args[0], nodeID, plan, size)
		if err != nil {
			return err
		}
		return app.Renderer().Render(v)
	})

	list := deprecatedList(newVolumesCmd(app))

	info := &cobra.Command{
		Use:   "info <name>",
		Short: "Show a volume and where it is mounted",
		Args:  cobra.ExactArgs(1),
		RunE: targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
			v, err := app.Portal().Volume(cmd.Context(), t.Org, t.Project, args[0])
			if err != nil {
				return err
			}
			r := app.Renderer()
			if err := r.Render(v); err != nil {
				return err
			}
			if len(v.Binds) == 0 {
				return nil
			}
			return r.Render(v.Binds)
		}),
	}

	bind := &cobra.Command{
		Use:   "bind <name> --app <app> --mount <path>",
		Short: "Mount a volume on an app and restart it",
		Args:  cobra.ExactArgs(1),
	}
	var bindApp, mount string
	var readOnly bool
	bind.Flags().StringVarP(&bindApp, "app", "a", "", "app to mount on (required)")
	bind.Flags().StringVar(&mount, "mount", "", "mount point inside the container (required)")
	bind.Flags().BoolVar(&readOnly, "read-only", false, "mount read-only")
	_ = bind.MarkFlagRequired("app")
	_ = bind.MarkFlagRequired("mount")
	bind.RunE = targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
		if err := app.Portal().BindVolume(cmd.Context(), t.Org, t.Project, args[0], bindApp, mount, readOnly); err != nil {
			return err
		}
		return app.Renderer().Message("Volume %s mounted on %s at %s.", args[0], bindApp, mount)
	})

	unbind := &cobra.Command{
		Use:   "unbind <name> --app <app> --mount <path>",
		Short: "Unmount a volume from an app and restart it",
		Args:  cobra.ExactArgs(1),
	}
	var unbindApp, unbindMount string
	unbind.Flags().StringVarP(&unbindApp, "app", "a", "", "app to unmount from (required)")
	unbind.Flags().StringVar(&unbindMount, "mount", "", "mount point to remove (required)")
	_ = unbind.MarkFlagRequired("app")
	_ = unbind.MarkFlagRequired("mount")
	unbind.RunE = targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
		if err := app.Portal().UnbindVolume(cmd.Context(), t.Org, t.Project, args[0], unbindApp, unbindMount); err != nil {
			return err
		}
		return app.Renderer().Message("Volume %s unmounted from %s. The data is still there.", args[0], unbindApp)
	})

	remove := &cobra.Command{
		Use:     "rm <name>",
		Aliases: []string{"remove", "delete"},
		Short:   "Delete a volume and the data on it",
		Args:    cobra.ExactArgs(1),
		RunE: targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
			if err := confirm(cmd, app.Global.Yes, "Delete volume %q and the data on it?", args[0]); err != nil {
				return err
			}
			if err := app.Portal().DeleteVolume(cmd.Context(), t.Org, t.Project, args[0]); err != nil {
				return err
			}
			return app.Renderer().Message("Volume %s deleted.", args[0])
		}),
	}

	cmd.AddCommand(create, list, info, bind, unbind, remove)
	return cmd
}

// resizeRow is a sizeRow with a marker for the size the node runs on.
type resizeRow struct {
	Slug     string `table:"SLUG"`
	VCPU     int    `table:"VCPU"`
	Memory   string `table:"MEMORY"`
	Disk     string `table:"DISK"`
	Provider string `table:"PROVIDER"`
	GoShip   string `table:"GOSHIP"`
	Current  string `table:"CURRENT"`
}

// newNodesCmd is the plural lister, matching `apps`.
func newNodesCmd(app *App) *cobra.Command {
	return newTargetListCmd(app, "nodes", "List your nodes in the current project, or --all of them", (*portal.Client).Nodes)
}

func newNodeCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "node",
		Short: "Manage your own machines (BYON)",
	}

	add := &cobra.Command{
		Use:        "add <name> --host <addr> --ssh-key <file>",
		Short:      "Connect a VPS you own",
		Args:       cobra.ExactArgs(1),
		Hidden:     true,
		Deprecated: "use goship node create --host",
	}
	var req portal.CreateNodeRequest
	var keyFile string
	add.Flags().StringVar(&req.Host, "host", "", "hostname or IP (required)")
	add.Flags().IntVar(&req.Port, "port", 22, "SSH port")
	add.Flags().StringVar(&req.SSHUser, "ssh-user", "root", "SSH user")
	add.Flags().StringVar(&keyFile, "ssh-key", "", "path to a private key with access to the host (required)")
	_ = add.MarkFlagRequired("host")
	_ = add.MarkFlagRequired("ssh-key")
	add.RunE = targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
		key, err := os.ReadFile(keyFile)
		if err != nil {
			return err
		}
		req.Name, req.Type, req.SSHKey = args[0], "vps", string(key)
		created, err := app.Portal().CreateNode(cmd.Context(), t.Org, t.Project, req)
		if err != nil {
			return err
		}
		return app.Renderer().Render(created)
	})

	list := deprecatedList(newNodesCmd(app))

	info := &cobra.Command{
		Use:   "info <node>",
		Short: "Show a node",
		Args:  cobra.ExactArgs(1),
		RunE: targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
			id, err := deploy.ResolveNode(cmd.Context(), app.Portal(), t, args[0])
			if err != nil {
				return err
			}
			n, err := app.Portal().Node(cmd.Context(), t.Org, t.Project, id)
			if err != nil {
				return err
			}
			if err := app.Renderer().Render(n); err != nil {
				return err
			}
			if app.Global.Output == render.JSON {
				return nil
			}
			if n.Status == "resizing" && n.Stage != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Resize stage: %s\n", n.Stage)
			}
			if n.ResizeMessage != "" {
				fmt.Fprintln(cmd.OutOrStdout(), n.ResizeMessage)
			}
			return nil
		}),
	}

	sizes := &cobra.Command{
		Use:   "sizes <node>",
		Short: "List the sizes a node can be resized to, with prices",
		Args:  cobra.ExactArgs(1),
		RunE: targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
			id, err := deploy.ResolveNode(cmd.Context(), app.Portal(), t, args[0])
			if err != nil {
				return err
			}
			options, err := app.Portal().NodeSizes(cmd.Context(), t.Org, t.Project, id)
			if err != nil {
				return err
			}
			if app.Global.Output == render.JSON {
				return app.Renderer().Render(options)
			}
			rows := make([]resizeRow, len(options))
			for i, o := range options {
				r := newSizeRow(o.CloudSize)
				rows[i] = resizeRow{Slug: r.Slug, VCPU: r.VCPU, Memory: r.Memory, Disk: r.Disk, Provider: r.Provider, GoShip: r.GoShip}
				if o.Current {
					rows[i].Current = "current"
				}
			}
			return app.Renderer().Render(rows)
		}),
	}

	var size string
	resize := &cobra.Command{
		Use:   "resize <node> --size <slug>",
		Short: "Change a node's machine size (see `goship node sizes`)",
		Args:  cobra.ExactArgs(1),
	}
	resize.Flags().StringVar(&size, "size", "", "machine size slug (required, see `goship node sizes`)")
	_ = resize.MarkFlagRequired("size")
	resize.RunE = targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
		id, err := deploy.ResolveNode(cmd.Context(), app.Portal(), t, args[0])
		if err != nil {
			return err
		}
		if err := confirm(cmd, app.Global.Yes, "Resize %s to %s? Every app on it is down for a few minutes.", args[0], size); err != nil {
			return err
		}
		if _, err := app.Portal().ResizeNode(cmd.Context(), t.Org, t.Project, id, size); err != nil {
			return deploy.ResizeReconnectHint(app.Portal().DashboardURL(), t.Org, t.Project, id, err)
		}
		return app.Renderer().Message("Node %s is resizing to %s. Follow it with `goship node info %s`.", args[0], size, args[0])
	})

	remove := &cobra.Command{
		Use:     "rm <node>",
		Aliases: []string{"remove", "delete"},
		Short:   "Disconnect a node",
		Args:    cobra.ExactArgs(1),
	}
	var destroy bool
	remove.Flags().BoolVar(&destroy, "destroy-server", false, "also destroy the machine at the cloud provider")
	remove.RunE = targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
		id, err := deploy.ResolveNode(cmd.Context(), app.Portal(), t, args[0])
		if err != nil {
			return err
		}
		question := "Disconnect node %q?"
		if destroy {
			question = "Disconnect node %q and destroy its machine at the cloud provider?"
		}
		if err := confirm(cmd, app.Global.Yes, question, args[0]); err != nil {
			return err
		}
		if err := app.Portal().DeleteNode(cmd.Context(), t.Org, t.Project, id, destroy); err != nil {
			return err
		}
		// Destroying the machine runs on after the API answers, so this says
		// it is underway, not done.
		if destroy {
			return app.Renderer().Message("Node %s is being removed; its machine will be destroyed — `goship nodes` shows when it is gone.", args[0])
		}
		return app.Renderer().Message("Node %s disconnected.", args[0])
	})

	cmd.AddCommand(add, newNodeCreateCmd(app), list, info, sizes, resize, remove)
	return cmd
}
