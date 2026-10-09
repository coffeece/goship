package mcptools

import (
	"context"
	"maps"
	"slices"

	"github.com/coffeece/goship/internal/portal"
)

type EnvList struct {
	Env []portal.EnvVar `json:"env"`
}

type SetEnvArgs struct {
	AppArg
	Env       map[string]string `json:"env" jsonschema:"variables to set, name to value"`
	Public    bool              `json:"public,omitempty" jsonschema:"mark them public (visible in the dashboard); secrets stay private by default"`
	NoRestart bool              `json:"no_restart,omitempty" jsonschema:"apply without restarting the app"`
}

type UnsetEnvArgs struct {
	AppArg
	Names     []string `json:"names" jsonschema:"variable names to remove"`
	NoRestart bool     `json:"no_restart,omitempty" jsonschema:"apply without restarting the app"`
}

func (r *registry) env() {
	tool(r, "goship_get_env", "List an app's environment variables. Private values are shown to the owner.", read,
		func(ctx context.Context, c *portal.Client, org string, in AppArg) (EnvList, error) {
			env, err := c.Env(ctx, org, in.App)
			return EnvList{Env: env}, err
		})
	tool(r, "goship_set_env", "Set environment variables on an app. Restarts it unless no_restart is true.", write,
		func(ctx context.Context, c *portal.Client, org string, in SetEnvArgs) (Message, error) {
			vars := make([]portal.EnvVar, 0, len(in.Env))
			for _, k := range slices.Sorted(maps.Keys(in.Env)) {
				vars = append(vars, portal.EnvVar{Name: k, Value: in.Env[k], Public: in.Public})
			}
			if err := c.SetEnv(ctx, org, in.App, vars, in.NoRestart); err != nil {
				return Message{}, err
			}
			return msgf("%d variable(s) set on %s.", len(vars), in.App)
		})
	tool(r, "goship_unset_env", "Remove environment variables from an app.", write,
		func(ctx context.Context, c *portal.Client, org string, in UnsetEnvArgs) (Message, error) {
			if err := c.UnsetEnv(ctx, org, in.App, in.Names, in.NoRestart); err != nil {
				return Message{}, err
			}
			return msgf("%d variable(s) removed from %s.", len(in.Names), in.App)
		})
}
