// CLI commands for app-to-app links. Talks to the Coffeece Portal REST API
// directly — Tsuru itself has no concept of these links. The token comes from
// the tsuru-client config (same bearer token, since Portal is the OIDC
// issuer for Tsuru).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/tsuru/go-tsuruclient/pkg/config"
	"github.com/tsuru/tsuru-client/tsuru/cmd"
)

const defaultPortalBase = "https://portal.coffeece.com"

// portalBaseURL is the Coffeece Portal API base. Override with COFFEECE_PORTAL.
func portalBaseURL() string {
	if v := os.Getenv("COFFEECE_PORTAL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return defaultPortalBase
}

// appLink mirrors entities.AppLink in portal/domain/entities. We don't import
// the portal module to keep the CLI binary slim.
type appLink struct {
	ID         string    `json:"id"`
	OrgID      string    `json:"org_id"`
	SourceApp  string    `json:"source_app"`
	TargetApp  string    `json:"target_app"`
	Alias      string    `json:"alias"`
	TargetProc string    `json:"target_proc"`
	TLSEnabled bool      `json:"tls_enabled"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type appLinkEnvVar struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Public bool   `json:"public"`
}

type appLinkCreateResponse struct {
	Link appLink         `json:"link"`
	Envs []appLinkEnvVar `json:"envs"`
}

// portalRequest issues an authenticated request to the Portal API. body may
// be nil; out may be nil if no JSON response is expected.
func portalRequest(method, path string, body, out any) error {
	token, err := config.DefaultTokenProvider.Token()
	if err != nil || token == "" {
		return fmt.Errorf("não autenticado — rode `coffeece login` antes")
	}

	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}

	req, err := http.NewRequest(method, portalBaseURL()+path, reqBody)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("calling portal: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		msg := strings.TrimSpace(string(b))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("portal %d: %s", resp.StatusCode, msg)
	}

	if out != nil && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// app-link-create
// ---------------------------------------------------------------------------

type appLinkCreate struct {
	org    string
	from   string
	to     string
	asName string
	fs     *pflag.FlagSet
}

func (c *appLinkCreate) Info() *cmd.Info {
	return &cmd.Info{
		Name:  "app-link-create",
		Usage: "--org <slug> --from <app> --to <app> [--as <ALIAS>]",
		Desc: `Conecta dois apps da mesma organização. As variáveis
<ALIAS>_URL, <ALIAS>_HOST e <ALIAS>_PORT são injetadas no app de origem,
apontando para o endereço interno do app de destino. Se --as for omitido,
o nome do app de destino é usado em UPPER_SNAKE_CASE.

Exemplo:
  coffeece app-link create --org acme --from web --to api --as BACKEND
`,
		MinArgs:          0,
		MaxArgs:          0,
		OnlyAppendOnRoot: true,
		GroupID:          "sub-resource",
	}
}

func (c *appLinkCreate) Flags() *pflag.FlagSet {
	if c.fs == nil {
		c.fs = pflag.NewFlagSet("app-link-create", pflag.ExitOnError)
		c.fs.StringVar(&c.org, "org", "", "Slug da organização (obrigatório)")
		c.fs.StringVar(&c.from, "from", "", "App que receberá as variáveis injetadas (obrigatório)")
		c.fs.StringVar(&c.to, "to", "", "App de destino — quem responde às chamadas (obrigatório)")
		c.fs.StringVar(&c.asName, "as", "", "Prefixo da variável (default: UPPER do --to)")
	}
	return c.fs
}

func (c *appLinkCreate) Run(ctx *cmd.Context) error {
	if c.org == "" || c.from == "" || c.to == "" {
		return fmt.Errorf("--org, --from e --to são obrigatórios")
	}
	alias := c.asName
	if alias == "" {
		alias = defaultAlias(c.to)
	}
	alias = strings.ToUpper(strings.TrimSpace(alias))

	path := fmt.Sprintf("/api/v1/orgs/%s/apps/%s/links",
		url.PathEscape(c.org), url.PathEscape(c.from))
	body := map[string]string{"target_app": c.to, "alias": alias}

	var resp appLinkCreateResponse
	if err := portalRequest(http.MethodPost, path, body, &resp); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(ctx.Stdout, "✓ link criado: %s → %s (alias %s)\n",
		c.from, c.to, resp.Link.Alias)
	_, _ = fmt.Fprintf(ctx.Stdout, "\nVariáveis injetadas em %q:\n", c.from)
	for _, e := range resp.Envs {
		_, _ = fmt.Fprintf(ctx.Stdout, "  %s=%s\n", e.Name, e.Value)
	}
	_, _ = fmt.Fprintln(ctx.Stdout, "\nO app foi reiniciado para aplicar as novas variáveis.")
	return nil
}

// ---------------------------------------------------------------------------
// app-link-list
// ---------------------------------------------------------------------------

type appLinkList struct {
	org       string
	app       string
	direction string
	fs        *pflag.FlagSet
}

func (c *appLinkList) Info() *cmd.Info {
	return &cmd.Info{
		Name:  "app-link-list",
		Usage: "--org <slug> --app <app> [--direction outbound|inbound]",
		Desc: `Lista os links de saída (default) ou de entrada para um app.
Saída: quais apps este consome. Entrada: quem consome este.

Exemplo:
  coffeece app-link list --org acme --app web
  coffeece app-link list --org acme --app api --direction inbound
`,
		MinArgs:          0,
		MaxArgs:          0,
		OnlyAppendOnRoot: true,
		GroupID:          "sub-resource",
	}
}

func (c *appLinkList) Flags() *pflag.FlagSet {
	if c.fs == nil {
		c.fs = pflag.NewFlagSet("app-link-list", pflag.ExitOnError)
		c.fs.StringVar(&c.org, "org", "", "Slug da organização (obrigatório)")
		c.fs.StringVar(&c.app, "app", "", "Nome do app (obrigatório)")
		c.fs.StringVar(&c.direction, "direction", "outbound", "outbound (saídas) ou inbound (entradas)")
	}
	return c.fs
}

func (c *appLinkList) Run(ctx *cmd.Context) error {
	if c.org == "" || c.app == "" {
		return fmt.Errorf("--org e --app são obrigatórios")
	}
	dir := strings.ToLower(strings.TrimSpace(c.direction))
	if dir != "outbound" && dir != "inbound" {
		return fmt.Errorf("--direction deve ser outbound ou inbound (recebi %q)", c.direction)
	}

	path := fmt.Sprintf("/api/v1/orgs/%s/apps/%s/links",
		url.PathEscape(c.org), url.PathEscape(c.app))
	if dir == "inbound" {
		path += "?direction=inbound"
	}

	var links []appLink
	if err := portalRequest(http.MethodGet, path, nil, &links); err != nil {
		return err
	}

	if len(links) == 0 {
		if dir == "inbound" {
			_, _ = fmt.Fprintf(ctx.Stdout, "nenhum app consome %q.\n", c.app)
		} else {
			_, _ = fmt.Fprintf(ctx.Stdout, "%q não consome nenhum app ainda.\n", c.app)
		}
		return nil
	}

	if dir == "inbound" {
		_, _ = fmt.Fprintf(ctx.Stdout, "Apps que consomem %q:\n", c.app)
		for _, l := range links {
			_, _ = fmt.Fprintf(ctx.Stdout, "  %s  (alias %s)\n", l.SourceApp, l.Alias)
		}
	} else {
		_, _ = fmt.Fprintf(ctx.Stdout, "Apps consumidos por %q:\n", c.app)
		for _, l := range links {
			_, _ = fmt.Fprintf(ctx.Stdout, "  %s → %s  (alias %s)\n", c.app, l.TargetApp, l.Alias)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// app-link-delete
// ---------------------------------------------------------------------------

type appLinkDelete struct {
	org    string
	from   string
	asName string
	fs     *pflag.FlagSet
}

func (c *appLinkDelete) Info() *cmd.Info {
	return &cmd.Info{
		Name:  "app-link-delete",
		Usage: "--org <slug> --from <app> --as <ALIAS>",
		Desc: `Remove um link de saída. As variáveis injetadas são apagadas
do app de origem e ele é reiniciado.

Exemplo:
  coffeece app-link delete --org acme --from web --as BACKEND
`,
		MinArgs:          0,
		MaxArgs:          0,
		OnlyAppendOnRoot: true,
		GroupID:          "sub-resource",
	}
}

func (c *appLinkDelete) Flags() *pflag.FlagSet {
	if c.fs == nil {
		c.fs = pflag.NewFlagSet("app-link-delete", pflag.ExitOnError)
		c.fs.StringVar(&c.org, "org", "", "Slug da organização (obrigatório)")
		c.fs.StringVar(&c.from, "from", "", "App de origem (obrigatório)")
		c.fs.StringVar(&c.asName, "as", "", "Prefixo da variável (obrigatório)")
	}
	return c.fs
}

func (c *appLinkDelete) Run(ctx *cmd.Context) error {
	if c.org == "" || c.from == "" || c.asName == "" {
		return fmt.Errorf("--org, --from e --as são obrigatórios")
	}
	alias := strings.ToUpper(strings.TrimSpace(c.asName))

	path := fmt.Sprintf("/api/v1/orgs/%s/apps/%s/links/%s",
		url.PathEscape(c.org), url.PathEscape(c.from), url.PathEscape(alias))

	if err := portalRequest(http.MethodDelete, path, nil, nil); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(ctx.Stdout, "✓ link %s removido de %s\n", alias, c.from)
	return nil
}

// defaultAlias converts an app name into a sensible default alias.
// "my-app" → "MY_APP", "api2" → "API2".
func defaultAlias(appName string) string {
	s := strings.ToUpper(appName)
	s = strings.ReplaceAll(s, "-", "_")
	return s
}
