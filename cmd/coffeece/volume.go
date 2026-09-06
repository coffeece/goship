// CLI commands for volumes on BYON nodes. They talk to the Coffeece Portal
// REST API, which validates the node/plan pairing and restarts the app on
// bind/unbind; Tsuru's own volume-* commands still work but skip those rules.
package main

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/spf13/pflag"
	"github.com/tsuru/tsuru-client/tsuru/cmd"
)

type portalVolume struct {
	Name        string `json:"name"`
	Plan        string `json:"plan"`
	CapacityGiB int    `json:"capacity_gib"`
	NodeName    string `json:"node_name"`
	NodeID      string `json:"node_id"`
	Binds       []struct {
		App        string `json:"app"`
		MountPoint string `json:"mount_point"`
		ReadOnly   bool   `json:"read_only"`
	} `json:"binds"`
}

// ---------------------------------------------------------------------------
// volume-create
// ---------------------------------------------------------------------------

type volumeCreate struct {
	org, node, plan string
	size            int
	fs              *pflag.FlagSet
}

func (c *volumeCreate) Info() *cmd.Info {
	return &cmd.Info{
		Name:  "volume-create",
		Usage: "<name> --org <slug> --node <node-id> [--plan byon-local|do-block-storage] [--size <GiB>]",
		Desc: `Cria um volume em um nó seu. O plano padrão é byon-local (disco da
máquina). Em um nó DigitalOcean com volumes habilitados use
--plan do-block-storage.

Exemplo:
  coffeece volume create uploads --org acme --node 9f3c... --plan do-block-storage --size 10
`,
		MinArgs: 1, MaxArgs: 1, GroupID: "sub-resource",
	}
}

func (c *volumeCreate) Flags() *pflag.FlagSet {
	if c.fs == nil {
		c.fs = pflag.NewFlagSet("volume-create", pflag.ExitOnError)
		c.fs.StringVar(&c.org, "org", "", "Slug da organização (obrigatório)")
		c.fs.StringVar(&c.node, "node", "", "ID do nó (obrigatório; veja no painel em Nós)")
		c.fs.StringVar(&c.plan, "plan", "byon-local", "byon-local ou do-block-storage")
		c.fs.IntVar(&c.size, "size", 5, "Tamanho em GiB")
	}
	return c.fs
}

func (c *volumeCreate) Run(ctx *cmd.Context) error {
	if c.org == "" || c.node == "" {
		return fmt.Errorf("--org e --node são obrigatórios")
	}
	var v portalVolume
	body := map[string]any{"name": ctx.Args[0], "node_id": c.node, "plan": c.plan, "capacity_gib": c.size}
	if err := portalRequest(http.MethodPost, "/api/v1/orgs/"+url.PathEscape(c.org)+"/volumes", body, &v); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(ctx.Stdout, "✓ volume %s criado no nó %s (%s, %d GiB)\n", v.Name, v.NodeName, v.Plan, v.CapacityGiB)
	_, _ = fmt.Fprintln(ctx.Stdout, "Monte em uma app com `coffeece volume bind`; o disco é criado no próximo deploy dela.")
	return nil
}

// ---------------------------------------------------------------------------
// volume-list
// ---------------------------------------------------------------------------

type volumeList struct {
	org string
	fs  *pflag.FlagSet
}

func (c *volumeList) Info() *cmd.Info {
	return &cmd.Info{Name: "volume-list", Usage: "--org <slug>", Desc: "Lista os volumes da organização.",
		MinArgs: 0, MaxArgs: 0, GroupID: "sub-resource"}
}

func (c *volumeList) Flags() *pflag.FlagSet {
	if c.fs == nil {
		c.fs = pflag.NewFlagSet("volume-list", pflag.ExitOnError)
		c.fs.StringVar(&c.org, "org", "", "Slug da organização (obrigatório)")
	}
	return c.fs
}

func (c *volumeList) Run(ctx *cmd.Context) error {
	if c.org == "" {
		return fmt.Errorf("--org é obrigatório")
	}
	var vs []portalVolume
	if err := portalRequest(http.MethodGet, "/api/v1/orgs/"+url.PathEscape(c.org)+"/volumes", nil, &vs); err != nil {
		return err
	}
	if len(vs) == 0 {
		_, _ = fmt.Fprintln(ctx.Stdout, "Nenhum volume.")
		return nil
	}
	_, _ = fmt.Fprintf(ctx.Stdout, "%-24s %-20s %-18s %6s  %s\n", "NOME", "NÓ", "PLANO", "GiB", "APPS")
	for _, v := range vs {
		_, _ = fmt.Fprintf(ctx.Stdout, "%-24s %-20s %-18s %6d  %d\n", v.Name, v.NodeName, v.Plan, v.CapacityGiB, len(v.Binds))
	}
	return nil
}

// ---------------------------------------------------------------------------
// volume-info
// ---------------------------------------------------------------------------

type volumeInfo struct {
	org string
	fs  *pflag.FlagSet
}

func (c *volumeInfo) Info() *cmd.Info {
	return &cmd.Info{Name: "volume-info", Usage: "<name> --org <slug>", Desc: "Mostra um volume e onde está montado.",
		MinArgs: 1, MaxArgs: 1, GroupID: "sub-resource"}
}

func (c *volumeInfo) Flags() *pflag.FlagSet {
	if c.fs == nil {
		c.fs = pflag.NewFlagSet("volume-info", pflag.ExitOnError)
		c.fs.StringVar(&c.org, "org", "", "Slug da organização (obrigatório)")
	}
	return c.fs
}

func (c *volumeInfo) Run(ctx *cmd.Context) error {
	if c.org == "" {
		return fmt.Errorf("--org é obrigatório")
	}
	var v portalVolume
	if err := portalRequest(http.MethodGet, "/api/v1/orgs/"+url.PathEscape(c.org)+"/volumes/"+url.PathEscape(ctx.Args[0]), nil, &v); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(ctx.Stdout, "Nome: %s\nNó: %s (%s)\nPlano: %s\nTamanho: %d GiB\n", v.Name, v.NodeName, v.NodeID, v.Plan, v.CapacityGiB)
	if len(v.Binds) == 0 {
		_, _ = fmt.Fprintln(ctx.Stdout, "Montado em: (nenhuma app)")
		return nil
	}
	_, _ = fmt.Fprintln(ctx.Stdout, "Montado em:")
	for _, b := range v.Binds {
		ro := ""
		if b.ReadOnly {
			ro = " (somente leitura)"
		}
		_, _ = fmt.Fprintf(ctx.Stdout, "  %s em %s%s\n", b.App, b.MountPoint, ro)
	}
	return nil
}

// ---------------------------------------------------------------------------
// volume-bind / volume-unbind
// ---------------------------------------------------------------------------

type volumeBind struct {
	org, app, mount string
	readOnly        bool
	fs              *pflag.FlagSet
}

func (c *volumeBind) Info() *cmd.Info {
	return &cmd.Info{Name: "volume-bind", Usage: "<name> --org <slug> --app <app> [--mount /data] [--read-only]",
		Desc:    "Monta o volume na app e a reinicia. A app precisa rodar no mesmo nó do volume.",
		MinArgs: 1, MaxArgs: 1, GroupID: "sub-resource"}
}

func (c *volumeBind) Flags() *pflag.FlagSet {
	if c.fs == nil {
		c.fs = pflag.NewFlagSet("volume-bind", pflag.ExitOnError)
		c.fs.StringVar(&c.org, "org", "", "Slug da organização (obrigatório)")
		c.fs.StringVar(&c.app, "app", "", "Nome da app (obrigatório)")
		c.fs.StringVar(&c.mount, "mount", "/data", "Ponto de montagem dentro do container")
		c.fs.BoolVar(&c.readOnly, "read-only", false, "Montar somente leitura")
	}
	return c.fs
}

func (c *volumeBind) Run(ctx *cmd.Context) error {
	if c.org == "" || c.app == "" {
		return fmt.Errorf("--org e --app são obrigatórios")
	}
	body := map[string]any{"app": c.app, "mount_point": c.mount, "read_only": c.readOnly}
	path := "/api/v1/orgs/" + url.PathEscape(c.org) + "/volumes/" + url.PathEscape(ctx.Args[0]) + "/bind"
	if err := portalRequest(http.MethodPost, path, body, nil); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(ctx.Stdout, "✓ %s montado em %s na app %s. A app foi reiniciada.\n", ctx.Args[0], c.mount, c.app)
	return nil
}

type volumeUnbind struct {
	org, app, mount string
	fs              *pflag.FlagSet
}

func (c *volumeUnbind) Info() *cmd.Info {
	return &cmd.Info{Name: "volume-unbind", Usage: "<name> --org <slug> --app <app> [--mount /data]",
		Desc:    "Desmonta o volume da app e a reinicia. Os dados continuam no volume.",
		MinArgs: 1, MaxArgs: 1, GroupID: "sub-resource"}
}

func (c *volumeUnbind) Flags() *pflag.FlagSet {
	if c.fs == nil {
		c.fs = pflag.NewFlagSet("volume-unbind", pflag.ExitOnError)
		c.fs.StringVar(&c.org, "org", "", "Slug da organização (obrigatório)")
		c.fs.StringVar(&c.app, "app", "", "Nome da app (obrigatório)")
		c.fs.StringVar(&c.mount, "mount", "/data", "Ponto de montagem a desmontar")
	}
	return c.fs
}

func (c *volumeUnbind) Run(ctx *cmd.Context) error {
	if c.org == "" || c.app == "" {
		return fmt.Errorf("--org e --app são obrigatórios")
	}
	body := map[string]string{"app": c.app, "mount_point": c.mount}
	path := "/api/v1/orgs/" + url.PathEscape(c.org) + "/volumes/" + url.PathEscape(ctx.Args[0]) + "/unbind"
	if err := portalRequest(http.MethodPost, path, body, nil); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(ctx.Stdout, "✓ %s desmontado da app %s. A app foi reiniciada.\n", ctx.Args[0], c.app)
	return nil
}

// ---------------------------------------------------------------------------
// volume-delete
// ---------------------------------------------------------------------------

type volumeDelete struct {
	org string
	yes bool
	fs  *pflag.FlagSet
}

func (c *volumeDelete) Info() *cmd.Info {
	return &cmd.Info{Name: "volume-delete", Usage: "<name> --org <slug> [-y]",
		Desc:    "Apaga o volume e os dados nele. Desmonte de todas as apps antes.",
		MinArgs: 1, MaxArgs: 1, GroupID: "sub-resource"}
}

func (c *volumeDelete) Flags() *pflag.FlagSet {
	if c.fs == nil {
		c.fs = pflag.NewFlagSet("volume-delete", pflag.ExitOnError)
		c.fs.StringVar(&c.org, "org", "", "Slug da organização (obrigatório)")
		c.fs.BoolVarP(&c.yes, "assume-yes", "y", false, "Não pedir confirmação")
	}
	return c.fs
}

func (c *volumeDelete) Run(ctx *cmd.Context) error {
	if c.org == "" {
		return fmt.Errorf("--org é obrigatório")
	}
	if !c.yes {
		_, _ = fmt.Fprintf(ctx.Stdout, "Apagar o volume %q e todos os dados nele? (y/N) ", ctx.Args[0])
		var answer string
		_, _ = fmt.Fscanln(ctx.Stdin, &answer)
		if answer != "y" && answer != "Y" {
			_, _ = fmt.Fprintln(ctx.Stdout, "Cancelado.")
			return nil
		}
	}
	path := "/api/v1/orgs/" + url.PathEscape(c.org) + "/volumes/" + url.PathEscape(ctx.Args[0])
	if err := portalRequest(http.MethodDelete, path, nil, nil); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(ctx.Stdout, "✓ volume %s apagado\n", ctx.Args[0])
	return nil
}
