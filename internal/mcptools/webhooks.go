package mcptools

import (
	"context"

	"github.com/coffeece/goship/internal/portal"
)

type WebhookList struct {
	Webhooks []portal.Webhook `json:"webhooks"`
	Events   []string         `json:"events" jsonschema:"the events a webhook can subscribe to"`
}

type CreateWebhookArgs struct {
	OrgArg
	URL    string   `json:"url" jsonschema:"https URL that receives the events; it must be reachable from the internet"`
	Events []string `json:"events" jsonschema:"events to be told about, from the events of goship_list_webhooks, e.g. deploy.failed"`
}

type WebhookIDArg struct {
	OrgArg
	ID string `json:"id" jsonschema:"webhook id from goship_list_webhooks"`
}

type WebhookDeliveriesArgs struct {
	WebhookIDArg
	Limit int `json:"limit,omitempty" jsonschema:"how many deliveries to return; 0 for the default"`
}

type WebhookDeliveryList struct {
	Deliveries []portal.WebhookDelivery `json:"deliveries"`
}

func (r *registry) webhooks() {
	orgTool(r, "goship_list_webhooks", "List an organization's webhooks (URLs told about deploys and app events) and the events one can subscribe to. Only the owner and admins can.", read,
		func(ctx context.Context, c *portal.Client, org string, _ OrgArg) (WebhookList, error) {
			hooks, events, err := c.WebhooksAndEvents(ctx, org)
			return WebhookList{Webhooks: hooks, Events: events}, err
		})
	orgTool(r, "goship_create_webhook", "Register a webhook. The signing secret is returned once, here only: give it to the user to store, it verifies the GoShip-Signature header of every delivery.", write,
		func(ctx context.Context, c *portal.Client, org string, in CreateWebhookArgs) (*portal.Webhook, error) {
			return c.CreateWebhook(ctx, org, in.URL, in.Events)
		})
	orgTool(r, "goship_delete_webhook", "Remove a webhook and its delivery history.", destructive,
		func(ctx context.Context, c *portal.Client, org string, in WebhookIDArg) (Message, error) {
			if err := c.DeleteWebhook(ctx, org, in.ID); err != nil {
				return Message{}, err
			}
			return msgf("Webhook removed.")
		})
	orgTool(r, "goship_ping_webhook", "Send the test event (ping) to a webhook. It is tried once within a few seconds; goship_list_webhook_deliveries shows how it went.", write,
		func(ctx context.Context, c *portal.Client, org string, in WebhookIDArg) (Message, error) {
			if err := c.PingWebhook(ctx, org, in.ID); err != nil {
				return Message{}, err
			}
			return msgf("Test event queued.")
		})
	orgTool(r, "goship_list_webhook_deliveries", "Show what was sent to a webhook, newest first: the event, whether it was delivered, the attempts, the HTTP status and the error.", read,
		func(ctx context.Context, c *portal.Client, org string, in WebhookDeliveriesArgs) (WebhookDeliveryList, error) {
			ds, err := c.WebhookDeliveries(ctx, org, in.ID, in.Limit)
			return WebhookDeliveryList{Deliveries: ds}, err
		})
}
