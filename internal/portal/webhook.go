package portal

import (
	"context"
	"strconv"
	"time"
)

// Webhook is a URL an organization registered to be told about events.
// Secret signs every delivery and is only set on the response that created
// the webhook.
type Webhook struct {
	ID        string    `json:"id" table:"ID"`
	URL       string    `json:"url" table:"URL"`
	Events    []string  `json:"events" table:"EVENTS"`
	CreatedBy string    `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at" table:"CREATED"`
	Secret    string    `json:"secret,omitempty"`
}

// WebhookDelivery is one event sent to one webhook, with how it went.
type WebhookDelivery struct {
	ID             string     `json:"id" table:"ID"`
	Event          string     `json:"event" table:"EVENT"`
	Status         string     `json:"status" table:"STATUS"`
	Attempts       int        `json:"attempts" table:"ATTEMPTS"`
	ResponseStatus int        `json:"response_status,omitempty" table:"HTTP"`
	Error          string     `json:"error,omitempty" table:"ERROR"`
	CreatedAt      time.Time  `json:"created_at" table:"CREATED"`
	NextAttemptAt  *time.Time `json:"next_attempt_at,omitempty" table:"NEXT TRY"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
}

func (c *Client) Webhooks(ctx context.Context, org string) ([]Webhook, error) {
	hooks, _, err := c.WebhooksAndEvents(ctx, org)
	return hooks, err
}

// WebhookEvents lists the events a webhook can subscribe to.
func (c *Client) WebhookEvents(ctx context.Context, org string) ([]string, error) {
	_, events, err := c.WebhooksAndEvents(ctx, org)
	return events, err
}

// WebhooksAndEvents returns the organization's webhooks and the events one
// can subscribe to; the API answers both in one call.
func (c *Client) WebhooksAndEvents(ctx context.Context, org string) ([]Webhook, []string, error) {
	var out struct {
		Webhooks []Webhook `json:"webhooks"`
		Events   []string  `json:"events"`
	}
	err := c.get(ctx, webhooksPath(org), &out)
	return out.Webhooks, out.Events, err
}

// CreateWebhook registers a webhook. The result carries the signing secret,
// which the API does not return again.
func (c *Client) CreateWebhook(ctx context.Context, org, url string, events []string) (*Webhook, error) {
	var out Webhook
	return &out, c.post(ctx, webhooksPath(org), map[string]any{"url": url, "events": events}, &out)
}

func (c *Client) DeleteWebhook(ctx context.Context, org, id string) error {
	return c.delete(ctx, webhooksPath(org)+"/"+esc(id), nil)
}

// PingWebhook queues the test event for one webhook.
func (c *Client) PingWebhook(ctx context.Context, org, id string) error {
	return c.post(ctx, webhooksPath(org)+"/"+esc(id)+"/ping", nil, nil)
}

// WebhookDeliveries returns what was sent to a webhook, newest first. limit 0
// leaves the number to the API.
func (c *Client) WebhookDeliveries(ctx context.Context, org, id string, limit int) ([]WebhookDelivery, error) {
	path := webhooksPath(org) + "/" + esc(id) + "/deliveries"
	if limit > 0 {
		path += "?limit=" + strconv.Itoa(limit)
	}
	var out struct {
		Deliveries []WebhookDelivery `json:"deliveries"`
	}
	return out.Deliveries, c.get(ctx, path, &out)
}

func webhooksPath(org string) string { return orgPath(org, "/webhooks") }
