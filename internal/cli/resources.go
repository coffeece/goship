package cli

import (
	"os"

	"github.com/coffeece/goship/internal/portal"
	"github.com/spf13/cobra"
)

// orgRunE adapts a command body that needs the resolved organization.
func orgRunE(app *App, fn func(cmd *cobra.Command, org string, args []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		org, err := app.Org()
		if err != nil {
			return err
		}
		return fn(cmd, org, args)
	}
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
	add.RunE = orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
		if err := app.Portal().AddAppDomain(cmd.Context(), org, addApp, args[0]); err != nil {
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
	remove.RunE = orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
		if err := app.Portal().RemoveAppDomain(cmd.Context(), org, rmApp, args[0]); err != nil {
			return err
		}
		return app.Renderer().Message("Domain %s detached from %s.", args[0], rmApp)
	})

	list := &cobra.Command{
		Use:   "list",
		Short: "List the organization's registered domains",
		Args:  cobra.NoArgs,
		RunE: orgRunE(app, func(cmd *cobra.Command, org string, _ []string) error {
			domains, err := app.Portal().OrgDomains(cmd.Context(), org)
			if err != nil {
				return err
			}
			return app.Renderer().Render(domains)
		}),
	}

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

func newVolumeCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "volume",
		Short: "Manage persistent disks on your own nodes",
	}

	create := &cobra.Command{
		Use:   "create <name> --node <node-id>",
		Short: "Create a volume on one of your nodes",
		Args:  cobra.ExactArgs(1),
	}
	var node, plan string
	var size int
	create.Flags().StringVar(&node, "node", "", "node id (required)")
	create.Flags().StringVar(&plan, "plan", "byon-local", "byon-local or do-block-storage")
	create.Flags().IntVar(&size, "size", 5, "capacity in GiB")
	_ = create.MarkFlagRequired("node")
	create.RunE = orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
		v, err := app.Portal().CreateVolume(cmd.Context(), org, args[0], node, plan, size)
		if err != nil {
			return err
		}
		return app.Renderer().Render(v)
	})

	list := &cobra.Command{
		Use:   "list",
		Short: "List volumes",
		Args:  cobra.NoArgs,
		RunE: orgRunE(app, func(cmd *cobra.Command, org string, _ []string) error {
			volumes, err := app.Portal().Volumes(cmd.Context(), org)
			if err != nil {
				return err
			}
			return app.Renderer().Render(volumes)
		}),
	}

	info := &cobra.Command{
		Use:   "info <name>",
		Short: "Show a volume and where it is mounted",
		Args:  cobra.ExactArgs(1),
		RunE: orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
			v, err := app.Portal().Volume(cmd.Context(), org, args[0])
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
	bind.RunE = orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
		if err := app.Portal().BindVolume(cmd.Context(), org, args[0], bindApp, mount, readOnly); err != nil {
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
	unbind.RunE = orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
		if err := app.Portal().UnbindVolume(cmd.Context(), org, args[0], unbindApp, unbindMount); err != nil {
			return err
		}
		return app.Renderer().Message("Volume %s unmounted from %s. The data is still there.", args[0], unbindApp)
	})

	remove := &cobra.Command{
		Use:     "rm <name>",
		Aliases: []string{"remove", "delete"},
		Short:   "Delete a volume and the data on it",
		Args:    cobra.ExactArgs(1),
		RunE: orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
			if err := confirm(cmd, app.Global.Yes, "Delete volume %q and the data on it?", args[0]); err != nil {
				return err
			}
			if err := app.Portal().DeleteVolume(cmd.Context(), org, args[0]); err != nil {
				return err
			}
			return app.Renderer().Message("Volume %s deleted.", args[0])
		}),
	}

	cmd.AddCommand(create, list, info, bind, unbind, remove)
	return cmd
}

func newNodeCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "node",
		Short: "Manage your own machines (BYON)",
	}

	add := &cobra.Command{
		Use:   "add <name> --host <addr> --ssh-key <file>",
		Short: "Connect a VPS you own",
		Args:  cobra.ExactArgs(1),
	}
	var req portal.CreateNodeRequest
	var keyFile string
	add.Flags().StringVar(&req.Host, "host", "", "hostname or IP (required)")
	add.Flags().IntVar(&req.Port, "port", 22, "SSH port")
	add.Flags().StringVar(&req.SSHUser, "ssh-user", "root", "SSH user")
	add.Flags().StringVar(&keyFile, "ssh-key", "", "path to a private key with access to the host (required)")
	_ = add.MarkFlagRequired("host")
	_ = add.MarkFlagRequired("ssh-key")
	add.RunE = orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
		key, err := os.ReadFile(keyFile)
		if err != nil {
			return err
		}
		req.Name, req.Type, req.SSHKey = args[0], "vps", string(key)
		created, err := app.Portal().CreateNode(cmd.Context(), org, req)
		if err != nil {
			return err
		}
		return app.Renderer().Render(created)
	})

	list := &cobra.Command{
		Use:   "list",
		Short: "List your nodes",
		Args:  cobra.NoArgs,
		RunE: orgRunE(app, func(cmd *cobra.Command, org string, _ []string) error {
			nodes, err := app.Portal().Nodes(cmd.Context(), org)
			if err != nil {
				return err
			}
			return app.Renderer().Render(nodes)
		}),
	}

	info := &cobra.Command{
		Use:   "info <id>",
		Short: "Show a node",
		Args:  cobra.ExactArgs(1),
		RunE: orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
			n, err := app.Portal().Node(cmd.Context(), org, args[0])
			if err != nil {
				return err
			}
			return app.Renderer().Render(n)
		}),
	}

	remove := &cobra.Command{
		Use:     "rm <id>",
		Aliases: []string{"remove", "delete"},
		Short:   "Disconnect a node",
		Args:    cobra.ExactArgs(1),
	}
	var destroy bool
	remove.Flags().BoolVar(&destroy, "destroy-server", false, "also destroy the machine at the cloud provider")
	remove.RunE = orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
		if err := confirm(cmd, app.Global.Yes, "Disconnect node %q?", args[0]); err != nil {
			return err
		}
		if err := app.Portal().DeleteNode(cmd.Context(), org, args[0], destroy); err != nil {
			return err
		}
		return app.Renderer().Message("Node %s disconnected.", args[0])
	})

	cmd.AddCommand(add, list, info, remove)
	return cmd
}
