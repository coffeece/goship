package mcptools

import (
	"context"
	"fmt"

	"github.com/coffeece/goship/internal/deploy"
	"github.com/coffeece/goship/internal/portal"
)

type AppArg struct {
	OrgArg
	App string `json:"app" jsonschema:"app name"`
}

type AppList struct {
	Apps []portal.App `json:"apps"`
}

type CreateAppArgs struct {
	OrgArg
	Name     string `json:"name" jsonschema:"app name: lowercase letters, digits and hyphens"`
	Platform string `json:"platform,omitempty" jsonschema:"go, python, nodejs or static; omit for an app built from a Dockerfile"`
	Plan     string `json:"plan,omitempty" jsonschema:"plan slug from goship_list_plans; required unless the org has a free plan or node is set"`
	Node     string `json:"node,omitempty" jsonschema:"name of one of the organization's own nodes to run the app on (never billed)"`
}

type ScaleArgs struct {
	AppArg
	Units int `json:"units" jsonschema:"number of units to run"`
}

type SetPlanArgs struct {
	AppArg
	Plan string `json:"plan" jsonschema:"plan slug from goship_list_plans"`
}

func (r *registry) apps() {
	tool(r, "goship_list_apps", "List the apps in an organization with status, plan and address.", read,
		func(ctx context.Context, c *portal.Client, org string, _ OrgArg) (AppList, error) {
			apps, err := c.Apps(ctx, org)
			return AppList{Apps: apps}, err
		})
	tool(r, "goship_get_app", "Show one app: platform, plan, units, addresses, custom domains and last error.", read,
		func(ctx context.Context, c *portal.Client, org string, in AppArg) (*portal.App, error) {
			return c.App(ctx, org, in.App)
		})
	tool(r, "goship_create_app", "Create an app without deploying it. goship_deploy creates the app itself when it is missing, so this is only for creating ahead of a deploy.", write,
		func(ctx context.Context, c *portal.Client, org string, in CreateAppArgs) (*portal.App, error) {
			req := portal.CreateAppRequest{Name: in.Name, Platform: in.Platform, Plan: in.Plan}
			if in.Node != "" {
				id, err := deploy.ResolveNode(ctx, c, org, in.Node)
				if err != nil {
					return nil, err
				}
				req.NodeID = &id
			} else if in.Plan == "" {
				chosen, err := deploy.ChoosePlan(ctx, c, org)
				if err != nil {
					return nil, err
				}
				req.Plan = chosen.Slug
			}
			return c.CreateApp(ctx, org, req)
		})
	tool(r, "goship_delete_app", "Delete an app, its releases and its routes.", destructive,
		func(ctx context.Context, c *portal.Client, org string, in AppArg) (Message, error) {
			if err := c.DeleteApp(ctx, org, in.App); err != nil {
				return Message{}, err
			}
			return msgf("App %s deleted.", in.App)
		})
	for _, action := range []string{"start", "stop", "restart"} {
		tool(r, "goship_"+action+"_app", fmt.Sprintf("%s an app's units.", capitalize(action)), write,
			func(ctx context.Context, c *portal.Client, org string, in AppArg) (Message, error) {
				if err := c.Lifecycle(ctx, org, in.App, action); err != nil {
					return Message{}, err
				}
				return msgf("App %s: %s requested.", in.App, action)
			})
	}
	tool(r, "goship_scale_app", "Set how many units of an app run.", write,
		func(ctx context.Context, c *portal.Client, org string, in ScaleArgs) (Message, error) {
			if err := c.ScaleApp(ctx, org, in.App, in.Units); err != nil {
				return Message{}, err
			}
			return msgf("App %s scaled to %d unit(s).", in.App, in.Units)
		})
	tool(r, "goship_set_app_plan", "Change an app's plan (CPU, memory and price). Takes effect on the next release.", write,
		func(ctx context.Context, c *portal.Client, org string, in SetPlanArgs) (Message, error) {
			if err := c.SetAppPlan(ctx, org, in.App, in.Plan); err != nil {
				return Message{}, err
			}
			return msgf("App %s moved to plan %s.", in.App, in.Plan)
		})
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return string(s[0]-'a'+'A') + s[1:]
}
