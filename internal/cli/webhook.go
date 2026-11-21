package cli

import (
	"fmt"
	"strings"

	"github.com/coffeece/goship/internal/portal"
	"github.com/coffeece/goship/internal/render"
	"github.com/spf13/cobra"
)

// newWebhooksCmd is the plural lister, matching `domains`.
func newWebhooksCmd(app *App) *cobra.Command {
	return newOrgListCmd(app, "webhooks", "List the webhooks of the current organization, or --all of them", (*portal.Client).Webhooks, skipForbiddenPlaces)
}

func newWebhookCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "webhook",
		Short: "Manage webhooks: URLs told about deploys and app events",
		Long: "A webhook is an https URL GoShip posts to when something happens in the\n" +
			"organization: a deploy starts, succeeds or fails, an app starts or stops, a\n" +
			"domain is verified. Each delivery is signed with the webhook's secret. Only\n" +
			"the owner and admins manage webhooks.",
	}

	add := &cobra.Command{
		Use:   "add <url> --event <event> [--event <event>...]",
		Short: "Register a webhook and print its signing secret once",
		Long: "Register an https URL. Name each event with --event, or pass --event all.\n" +
			"`goship webhook events` lists them.",
		Args: cobra.ExactArgs(1),
	}
	var events []string
	add.Flags().StringSliceVarP(&events, "event", "e", nil, "event to be told about; repeat it, separate with commas, or pass all (required)")
	_ = add.MarkFlagRequired("event")
	add.RunE = orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
		ctx, client := cmd.Context(), app.Portal()
		if len(events) == 1 && events[0] == "all" {
			known, err := client.WebhookEvents(ctx, org)
			if err != nil {
				return err
			}
			events = known
		}
		created, err := client.CreateWebhook(ctx, org, args[0], events)
		if err != nil {
			return err
		}
		if app.Global.Output != render.Table {
			return app.Renderer().Render(created)
		}
		// The secret alone on stdout, so a script can capture it; the rest
		// goes to stderr.
		fmt.Fprintln(cmd.OutOrStdout(), created.Secret)
		fmt.Fprintf(cmd.ErrOrStderr(), "Webhook %s registered for %s.\nThis is the only time the signing secret is shown. Store it now.\n", created.ID, strings.Join(created.Events, ", "))
		return nil
	})

	eventsCmd := &cobra.Command{
		Use:   "events",
		Short: "List the events a webhook can be told about",
		Args:  cobra.NoArgs,
		RunE: orgRunE(app, func(cmd *cobra.Command, org string, _ []string) error {
			known, err := app.Portal().WebhookEvents(cmd.Context(), org)
			if err != nil {
				return err
			}
			if app.Global.Output != render.Table {
				return app.Renderer().Render(known)
			}
			for _, e := range known {
				fmt.Fprintln(cmd.OutOrStdout(), e)
			}
			return nil
		}),
	}

	remove := &cobra.Command{
		Use:     "rm <webhook-id>",
		Aliases: []string{"remove"},
		Short:   "Remove a webhook and its delivery history",
		Args:    cobra.ExactArgs(1),
		RunE: orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
			if err := app.Portal().DeleteWebhook(cmd.Context(), org, args[0]); err != nil {
				return err
			}
			return app.Renderer().Message("Webhook %s removed.", args[0])
		}),
	}

	ping := &cobra.Command{
		Use:   "ping <webhook-id>",
		Short: "Send the test event to a webhook",
		Long: "Queue a `ping` event for the webhook. It is tried once, within a few\n" +
			"seconds; `goship webhook deliveries` shows how it went.",
		Args: cobra.ExactArgs(1),
		RunE: orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
			if err := app.Portal().PingWebhook(cmd.Context(), org, args[0]); err != nil {
				return err
			}
			return app.Renderer().Message("Test event queued for %s. See `goship webhook deliveries %s`.", args[0], args[0])
		}),
	}

	deliveries := &cobra.Command{
		Use:   "deliveries <webhook-id>",
		Short: "Show what was sent to a webhook and how it went, newest first",
		Args:  cobra.ExactArgs(1),
	}
	var limit int
	deliveries.Flags().IntVarP(&limit, "limit", "n", 20, "how many deliveries to show")
	deliveries.RunE = orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
		ds, err := app.Portal().WebhookDeliveries(cmd.Context(), org, args[0], limit)
		if err != nil {
			return err
		}
		return app.Renderer().Render(ds)
	})

	list := deprecatedList(newWebhooksCmd(app))
	list.Aliases = []string{"ls"}

	cmd.AddCommand(add, eventsCmd, list, remove, ping, deliveries)
	return cmd
}
