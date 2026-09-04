package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/spf13/pflag"
	"github.com/tsuru/go-tsuruclient/pkg/config"
	"github.com/tsuru/tsuru-client/tsuru/client"
	"github.com/tsuru/tsuru-client/tsuru/cmd"
	tsuruHTTP "github.com/tsuru/tsuru-client/tsuru/http"
	tsuruErrors "github.com/tsuru/tsuru/errors"
)

// coffeeceDeploy is the headline command: it collapses app-create + env-set +
// app-deploy into one step. It composes the real tsuru-client commands — the
// archive/upload/stream logic is reused, not reimplemented. Authentication is
// set up by the manager's AfterFlagParseHook before Run is called.
type coffeeceDeploy struct {
	app        string
	platform   string
	plan       string
	pool       string
	team       string
	message    string
	configPath string
	envFile    string
	fs         *pflag.FlagSet
}

func (c *coffeeceDeploy) Info() *cmd.Info {
	return &cmd.Info{
		Name:  "deploy",
		Usage: "[diretório] [--app a] [--platform p] [--env-file arquivo] [--plan p] [--pool o] [--team t] [--message m]",
		Desc: `Cria a app (se ainda não existir), aplica variáveis de ambiente e faz
o deploy — tudo num único comando.

Variáveis de ambiente vêm de coffeece.yaml (campo "env:", configuração
pública) e/ou de um arquivo --env-file (segredos, definidos como privados).
O diretório padrão de deploy é o atual.

Exemplos:
  coffeece deploy
  coffeece deploy --app meu-agente --platform python --env-file .env
  coffeece deploy ./build`,
		MinArgs:          0,
		MaxArgs:          1,
		OnlyAppendOnRoot: true,
		GroupID:          "resource",
	}
}

func (c *coffeeceDeploy) Flags() *pflag.FlagSet {
	if c.fs == nil {
		c.fs = pflag.NewFlagSet("deploy", pflag.ExitOnError)
		c.fs.StringVarP(&c.app, "app", "a", "", "Nome da app (sobrepõe coffeece.yaml)")
		c.fs.StringVar(&c.platform, "platform", "", "Plataforma, usada ao criar a app (ex: python, go, nodejs)")
		c.fs.StringVar(&c.plan, "plan", "", "Plano, usado ao criar a app")
		c.fs.StringVar(&c.pool, "pool", "", "Pool, usado ao criar a app")
		c.fs.StringVar(&c.team, "team", "", "Time dono, usado ao criar a app")
		c.fs.StringVarP(&c.message, "message", "m", "", "Mensagem do deploy")
		c.fs.StringVar(&c.configPath, "config", "coffeece.yaml", "Arquivo de configuração do projeto")
		c.fs.StringVar(&c.envFile, "env-file", "", "Arquivo .env; variáveis são definidas como privadas")
	}
	return c.fs
}

func (c *coffeeceDeploy) Run(ctx *cmd.Context) error {
	dir := "."
	if len(ctx.Args) == 1 {
		dir = ctx.Args[0]
	}

	// 1. Project config (coffeece.yaml — optional). Flags override it.
	proj, err := loadProjectConfig(c.configPath)
	if err != nil {
		return err
	}
	app := firstNonEmpty(c.app, proj.App)
	if app == "" {
		return fmt.Errorf("nome da app não definido — use --app ou o campo `app:` no coffeece.yaml")
	}
	platform := firstNonEmpty(c.platform, proj.Platform)
	plan := firstNonEmpty(c.plan, proj.Plan)
	pool := firstNonEmpty(c.pool, proj.Pool)
	team := firstNonEmpty(c.team, proj.Team)

	// 2. Env vars: coffeece.yaml `env:` (public) + --env-file (private).
	//    --env-file wins on key conflict.
	publicEnv := map[string]string{}
	for k, v := range proj.Env {
		publicEnv[k] = v
	}
	privateEnv := map[string]string{}
	if c.envFile != "" {
		privateEnv, err = parseEnvFile(c.envFile)
		if err != nil {
			return err
		}
	}
	for k := range privateEnv {
		delete(publicEnv, k)
	}

	// 3. Ensure the app exists.
	exists, err := appExists(app)
	if err != nil {
		return err
	}
	if !exists {
		if platform == "" {
			return fmt.Errorf("a app %q não existe e nenhuma plataforma foi informada — use --platform ou `platform:` no coffeece.yaml", app)
		}
		fmt.Fprintf(ctx.Stdout, "→ criando app %q (%s)\n", app, platform)
		if err := runAppCreate(ctx, app, platform, plan, pool, team); err != nil {
			return fmt.Errorf("criando a app: %w", err)
		}
	}

	// 4. Apply env vars without restarting — the deploy in step 5 restarts.
	if len(publicEnv) > 0 {
		fmt.Fprintf(ctx.Stdout, "→ aplicando %d variável(is) de ambiente\n", len(publicEnv))
		if err := runEnvSet(ctx, app, publicEnv, false); err != nil {
			return fmt.Errorf("definindo variáveis: %w", err)
		}
	}
	if len(privateEnv) > 0 {
		fmt.Fprintf(ctx.Stdout, "→ aplicando %d variável(is) privada(s)\n", len(privateEnv))
		if err := runEnvSet(ctx, app, privateEnv, true); err != nil {
			return fmt.Errorf("definindo variáveis privadas: %w", err)
		}
	}

	// 5. Deploy.
	fmt.Fprintf(ctx.Stdout, "→ deploy de %q\n", app)
	if err := runAppDeploy(ctx, app, dir, c.message); err != nil {
		return err
	}

	// 6. Report the live URL (best-effort).
	if url := appURL(app); url != "" {
		fmt.Fprintf(ctx.Stdout, "\n✓ no ar: %s\n", url)
	} else {
		fmt.Fprintf(ctx.Stdout, "\n✓ deploy de %q concluído\n", app)
	}
	return nil
}

// childContext builds a cmd.Context for a composed sub-command, inheriting the
// parent's streams but with its own args.
func childContext(parent *cmd.Context, args []string) *cmd.Context {
	return &cmd.Context{
		Args:   args,
		Stdout: parent.Stdout,
		Stderr: parent.Stderr,
		Stdin:  parent.Stdin,
	}
}

// runAppCreate composes the tsuru-client `app-create` command.
func runAppCreate(parent *cmd.Context, app, platform, plan, pool, team string) error {
	c := &client.AppCreate{}
	args := []string{}
	if plan != "" {
		args = append(args, "--plan", plan)
	}
	if pool != "" {
		args = append(args, "--pool", pool)
	}
	if team != "" {
		args = append(args, "--team", team)
	}
	if err := c.Flags().Parse(args); err != nil {
		return err
	}
	return c.Run(childContext(parent, []string{app, platform}))
}

// runEnvSet composes the tsuru-client `env-set` command. NoRestart is always
// set — the deploy that follows performs the restart.
func runEnvSet(parent *cmd.Context, app string, env map[string]string, private bool) error {
	c := &client.EnvSet{}
	args := []string{"--app", app, "--no-restart"}
	if private {
		args = append(args, "--private")
	}
	if err := c.Flags().Parse(args); err != nil {
		return err
	}
	pairs := make([]string, 0, len(env))
	for k, v := range env {
		pairs = append(pairs, k+"="+v)
	}
	sort.Strings(pairs)
	return c.Run(childContext(parent, pairs))
}

// runAppDeploy composes the tsuru-client `app-deploy` command, reusing its
// archive/upload/stream logic.
func runAppDeploy(parent *cmd.Context, app, dir, message string) error {
	c := &client.AppDeploy{}
	args := []string{"--app", app}
	if message != "" {
		args = append(args, "--message", message)
	}
	if err := c.Flags().Parse(args); err != nil {
		return err
	}
	err := c.Run(childContext(parent, []string{dir}))
	if err == cmd.ErrAbortCommand {
		return fmt.Errorf("o deploy de %q falhou", app)
	}
	return err
}

// appExists reports whether the app is already provisioned. A 404 means "no",
// any other failure is surfaced.
func appExists(app string) (bool, error) {
	u, err := config.GetURL("/apps/" + app)
	if err != nil {
		return false, err
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return false, err
	}
	resp, err := tsuruHTTP.AuthenticatedClient.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		return true, nil
	}
	switch httpStatus(err) {
	case http.StatusNotFound:
		return false, nil
	case http.StatusUnauthorized:
		return false, fmt.Errorf("não autenticado — rode `coffeece login`")
	default:
		return false, fmt.Errorf("verificando a app %q: %w", app, err)
	}
}

// httpStatus extracts the HTTP status code from a tsuru client error, or 0.
func httpStatus(err error) int {
	if httpErr, ok := tsuruHTTP.UnwrapErr(err).(*tsuruErrors.HTTP); ok {
		return httpErr.StatusCode()
	}
	return 0
}

// appURL fetches the app's public address. Best-effort: returns "" on any error.
func appURL(app string) string {
	u, err := config.GetURL("/apps/" + app)
	if err != nil {
		return ""
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return ""
	}
	resp, err := tsuruHTTP.AuthenticatedClient.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	var info struct {
		Routers []struct {
			Address   string   `json:"address"`
			Addresses []string `json:"addresses"`
		} `json:"routers"`
	}
	if json.NewDecoder(resp.Body).Decode(&info) != nil {
		return ""
	}
	for _, r := range info.Routers {
		if r.Address != "" {
			return ensureScheme(r.Address)
		}
		for _, a := range r.Addresses {
			if a != "" {
				return ensureScheme(a)
			}
		}
	}
	return ""
}

func ensureScheme(addr string) string {
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return addr
	}
	return "https://" + addr
}
