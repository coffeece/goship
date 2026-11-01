package cli

import (
	"fmt"

	"github.com/coffeece/goship/internal/deploy"
	"github.com/coffeece/goship/internal/portal"
	"github.com/coffeece/goship/internal/render"
	"github.com/spf13/cobra"
)

// resizeRow is a sizeRow with a marker for the size the cluster runs on.
type resizeRow struct {
	Slug     string `table:"SLUG"`
	VCPU     int    `table:"VCPU"`
	Memory   string `table:"MEMORY"`
	Disk     string `table:"DISK"`
	Provider string `table:"PROVIDER"`
	GoShip   string `table:"GOSHIP"`
	Current  string `table:"CURRENT"`
}

// newClustersCmd is the plural lister, matching `apps`.
func newClustersCmd(app *App) *cobra.Command {
	return newOrgListCmd(app, "clusters", "List the clusters the current organization can use, its own and GoShip's shared ones, or --all of them", (*portal.Client).Clusters)
}

func newClusterCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Manage the clusters your environments run on (your own machines or Kubernetes)",
	}

	info := &cobra.Command{
		Use:   "info <cluster>",
		Short: "Show a cluster",
		Args:  cobra.ExactArgs(1),
		RunE: orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
			id, err := deploy.ResolveCluster(cmd.Context(), app.Portal(), org, args[0])
			if err != nil {
				return err
			}
			c, err := app.Portal().Cluster(cmd.Context(), org, id)
			if err != nil {
				return err
			}
			if err := app.Renderer().Render(c); err != nil {
				return err
			}
			if app.Global.Output == render.JSON {
				return nil
			}
			if c.Status == "resizing" && c.Stage != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Resize stage: %s\n", c.Stage)
			}
			if c.ResizeMessage != "" {
				fmt.Fprintln(cmd.OutOrStdout(), c.ResizeMessage)
			}
			return nil
		}),
	}

	sizes := &cobra.Command{
		Use:   "sizes <cluster>",
		Short: "List the sizes a cluster's machine can be resized to, with prices",
		Args:  cobra.ExactArgs(1),
		RunE: orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
			id, err := deploy.ResolveCluster(cmd.Context(), app.Portal(), org, args[0])
			if err != nil {
				return err
			}
			options, err := app.Portal().ClusterSizes(cmd.Context(), org, id)
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
		Use:   "resize <cluster> --size <slug>",
		Short: "Change a cluster's machine size (see `goship cluster sizes`)",
		Args:  cobra.ExactArgs(1),
	}
	resize.Flags().StringVar(&size, "size", "", "machine size slug (required, see `goship cluster sizes`)")
	_ = resize.MarkFlagRequired("size")
	resize.RunE = orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
		id, err := deploy.ResolveCluster(cmd.Context(), app.Portal(), org, args[0])
		if err != nil {
			return err
		}
		if err := confirm(cmd, app.Global.Yes, "Resize cluster %q of %s to %s? Every app on it is down for a few minutes.", args[0], org, size); err != nil {
			return err
		}
		if _, err := app.Portal().ResizeCluster(cmd.Context(), org, id, size); err != nil {
			return deploy.ResizeReconnectHint(app.Portal().DashboardURL(), org, id, err)
		}
		return app.Renderer().Message("Cluster %s is resizing to %s. Follow it with `goship cluster info %s`.", args[0], size, args[0])
	})

	remove := &cobra.Command{
		Use:     "rm <cluster>",
		Aliases: []string{"remove", "delete"},
		Short:   "Disconnect a cluster",
		Args:    cobra.ExactArgs(1),
	}
	var destroy bool
	remove.Flags().BoolVar(&destroy, "destroy-server", false, "also destroy the machine at the cloud provider")
	remove.RunE = orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
		id, err := deploy.ResolveCluster(cmd.Context(), app.Portal(), org, args[0])
		if err != nil {
			return err
		}
		question := "Disconnect cluster %q of %s?"
		if destroy {
			question = "Disconnect cluster %q of %s and destroy its machine at the cloud provider?"
		}
		if err := confirm(cmd, app.Global.Yes, question, args[0], org); err != nil {
			return err
		}
		if err := app.Portal().DeleteCluster(cmd.Context(), org, id, destroy); err != nil {
			return err
		}
		// Destroying the machine runs on after the API answers, so this says
		// it is underway, not done.
		if destroy {
			return app.Renderer().Message("Cluster %s is being removed; its machine will be destroyed — `goship clusters` shows when it is gone.", args[0])
		}
		return app.Renderer().Message("Cluster %s disconnected.", args[0])
	})

	cmd.AddCommand(newClusterCreateCmd(app), info, sizes, resize, remove)
	return cmd
}

// ExitCode ends the process with that status. The command already said why
// on stderr, so there is nothing more to print.
type ExitCode int

func (c ExitCode) Error() string { return fmt.Sprintf("exit status %d", int(c)) }

// newMovedNodeCmds keep `goship node …` and `goship nodes` from failing as
// unknown commands: hidden, they take any arguments and point at `cluster`.
func newMovedNodeCmds() []*cobra.Command {
	moved := func(use string) *cobra.Command {
		return &cobra.Command{
			Use:                use,
			Hidden:             true,
			DisableFlagParsing: true,
			Args:               cobra.ArbitraryArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				fmt.Fprintln(cmd.ErrOrStderr(), `node commands are now "goship cluster …"`)
				return ExitCode(2)
			},
		}
	}
	return []*cobra.Command{moved("node"), moved("nodes")}
}
