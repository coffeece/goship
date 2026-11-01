package cli

import (
	"github.com/coffeece/goship/internal/portal"
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
		if err := app.Portal().AddAppDomain(cmd.Context(), t.Org, t.Project, t.Environment, addApp, args[0]); err != nil {
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
		if err := app.Portal().RemoveAppDomain(cmd.Context(), t.Org, t.Project, t.Environment, rmApp, args[0]); err != nil {
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
		Short: "Manage persistent disks on your own clusters",
	}

	create := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a volume on the cluster the environment runs on (one of your own)",
		Args:  cobra.ExactArgs(1),
	}
	var plan string
	var size int
	create.Flags().StringVar(&plan, "plan", "byon-local", "byon-local or do-block-storage")
	create.Flags().IntVar(&size, "size", 5, "capacity in GiB")
	create.RunE = targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
		v, err := app.Portal().CreateVolume(cmd.Context(), t.Org, t.Project, t.Environment, args[0], plan, size)
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
			v, err := app.Portal().Volume(cmd.Context(), t.Org, t.Project, t.Environment, args[0])
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
		if err := app.Portal().BindVolume(cmd.Context(), t.Org, t.Project, t.Environment, args[0], bindApp, mount, readOnly); err != nil {
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
		if err := app.Portal().UnbindVolume(cmd.Context(), t.Org, t.Project, t.Environment, args[0], unbindApp, unbindMount); err != nil {
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
			if err := app.Portal().DeleteVolume(cmd.Context(), t.Org, t.Project, t.Environment, args[0]); err != nil {
				return err
			}
			return app.Renderer().Message("Volume %s deleted.", args[0])
		}),
	}

	cmd.AddCommand(create, list, info, bind, unbind, remove)
	return cmd
}
