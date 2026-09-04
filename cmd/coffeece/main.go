// Command coffeece is the Coffeece CLI: a curated, user-facing build of the
// Tsuru client. It exposes the commands a developer needs to ship an app or
// agent — login, app, env, service, job, deploy — plus the composite
// `coffeece deploy` (create + env + deploy in one step). Operator/admin
// commands (platform, cluster, role, quota, …) are deliberately left out.
//
// It reuses tsuru-client's command code directly (same Go module); the target
// is pinned to the Coffeece API so users never run `tsuru target-add`.
package main

import (
	"fmt"
	"io"
	"os"

	goTsuruClient "github.com/tsuru/go-tsuruclient/pkg/client"
	"github.com/tsuru/go-tsuruclient/pkg/config"
	"github.com/tsuru/tsuru-client/tsuru/auth"
	"github.com/tsuru/tsuru-client/tsuru/client"
	"github.com/tsuru/tsuru-client/tsuru/cmd"
	tsuruHTTP "github.com/tsuru/tsuru-client/tsuru/http"
)

// version is overridden at build time via -ldflags.
var version = "dev"

// defaultTarget is the Coffeece API. Pinning it via TSURU_TARGET means users
// never have to run `tsuru target-add`. An explicit TSURU_TARGET still wins.
const defaultTarget = "https://api.coffeece.com"

func main() {
	if os.Getenv("TSURU_TARGET") == "" {
		_ = os.Setenv("TSURU_TARGET", defaultTarget)
	}
	defer config.SaveChangesWithTimeout()

	m := buildManager(os.Stdout, os.Stderr)
	if err := m.Run(); err != nil {
		os.Exit(1)
	}
}

// initAuthorization wires up the package-level authenticated HTTP client every
// reused tsuru-client command depends on. Mirrors tsuru-client's own setup.
func initAuthorization() {
	roundTripper, tokenProvider, err := goTsuruClient.RoundTripperAndTokenProvider()
	if err != nil {
		fmt.Fprintf(os.Stderr, "não foi possível ler o token de autenticação: %v\n", err)
		os.Exit(1)
	}
	tsuruHTTP.AuthenticatedClient = tsuruHTTP.NewTerminalClient(tsuruHTTP.TerminalClientOptions{
		RoundTripper:  roundTripper,
		ClientName:    "coffeece",
		ClientVersion: version,
		Stdout:        os.Stdout,
		Stderr:        os.Stderr,
	})
	config.DefaultTokenProvider = tokenProvider
}

// buildManager registers the curated user-facing command set. Admin/operator
// commands from the tsuru-client `admin` package are intentionally excluded.
func buildManager(stdout, stderr io.Writer) *cmd.ManagerV2 {
	m := cmd.NewManagerV2(&cmd.ManagerV2Opts{
		AfterFlagParseHook: func() { initAuthorization() },
	})

	// Rebrand the root: the tsuru-client manager hardcodes "tsuru".
	root := m.Cobra()
	root.Use = "coffeece"
	root.Short = "Coffeece — crie, configure e faça deploy de apps e agentes de IA"

	m.Register(&auth.Login{})
	m.Register(&auth.Logout{})
	m.Register(&coffeeceVersion{})

	// The headline command — create + env + deploy in one step.
	m.Register(&coffeeceDeploy{})

	m.RegisterTopic("app", "App é o código da sua aplicação rodando na Coffeece.")
	m.Register(&client.AppCreate{})
	m.Register(&client.AppInfo{})
	m.Register(&client.AppList{})
	m.Register(&client.AppUpdate{})
	m.Register(&client.AppRemove{})
	m.Register(&client.AppLog{})
	m.Register(&client.AppRestart{})
	m.Register(&client.AppStart{})
	m.Register(&client.AppStop{})
	m.Register(&client.AppRun{})
	m.Register(&client.AppGrant{})
	m.Register(&client.AppRevoke{})
	m.Register(&client.ShellToContainerCmd{})
	m.Register(&client.Init{})
	m.Register(&client.AppProcessUpdate{})
	m.Register(&client.AppDeployList{})
	m.Register(&client.AppDeployRollback{})

	m.RegisterTopic("app-link", "Conecte apps da mesma organização via variáveis de ambiente.")
	m.Register(&appLinkCreate{})
	m.Register(&appLinkList{})
	m.Register(&appLinkDelete{})

	m.RegisterTopic("unit", "Uma unit é um container da sua app.")
	m.Register(&client.UnitAdd{})
	m.Register(&client.UnitRemove{})
	m.Register(&client.UnitKill{})
	m.Register(&client.UnitSet{})
	m.Register(&client.AppVersionRemove{})

	m.RegisterTopic("env", "Gerencie variáveis de ambiente de uma app ou job.")
	m.Register(&client.EnvGet{})
	m.Register(&client.EnvSet{})
	m.Register(&client.EnvUnset{})

	m.RegisterTopic("service", "Serviços são recursos gerenciados (banco, cache) ligados à app.")
	m.Register(&client.ServiceList{})
	m.RegisterTopic("service-instance", "Gerencie recursos provisionados ligados à app.")
	m.Register(&client.ServiceInstanceAdd{})
	m.Register(&client.ServiceInstanceUpdate{})
	m.Register(&client.ServiceInstanceRemove{})
	m.Register(&client.ServiceInstanceInfo{})
	m.Register(&client.ServiceInfo{})
	m.RegisterTopic("service-plan", "Planos de um serviço.")
	m.Register(&client.ServicePlanList{})
	m.Register(&client.ServiceInstanceGrant{})
	m.Register(&client.ServiceInstanceRevoke{})
	m.Register(&client.ServiceInstanceBind{})
	m.Register(&client.ServiceInstanceUnbind{})

	m.RegisterTopic("job", "Job é um programa que roda agendado ou sob demanda.")
	m.Register(&client.JobCreate{})
	m.Register(&client.JobUpdate{})
	m.Register(&client.JobInfo{})
	m.Register(&client.JobList{})
	m.Register(&client.JobDelete{})
	m.Register(&client.JobTrigger{})
	m.Register(&client.JobLog{})
	m.Register(&client.JobDeploy{})

	m.RegisterTopic("plan", "Planos definem os recursos computacionais da app.")
	m.Register(&client.PlanList{})

	m.RegisterTopic("cname", "CNAME é um domínio customizado para a app.")
	m.Register(&client.CnameAdd{})
	m.Register(&client.CnameRemove{})

	m.RegisterTopic("certificate", "Gerencie certificados TLS da app.")
	m.Register(&client.CertificateSet{})
	m.Register(&client.CertificateUnset{})
	m.Register(&client.CertificateList{})

	m.RegisterTopic("plugin", "Plugins estendem a funcionalidade do CLI.")
	m.Register(&client.PluginInstall{})
	m.Register(&client.PluginRemove{})
	m.Register(&client.PluginList{})
	m.Register(&client.PluginBundle{})

	m.Register(&client.ChangePassword{})
	m.Register(client.UserInfo{})

	// Shorthands — short names for frequent commands.
	m.RegisterShorthand(&client.AppDeployRollback{}, "rollback")
	m.RegisterShorthand(&client.AppLog{}, "log")
	m.RegisterShorthand(&client.AppInfo{}, "info")
	m.RegisterShorthand(&client.ShellToContainerCmd{}, "shell")
	m.RegisterHiddenShorthand(&client.AppList{}, "list")
	m.RegisterHiddenShorthand(&client.AppRestart{}, "restart")
	m.RegisterHiddenShorthand(&client.AppStart{}, "start")
	m.RegisterHiddenShorthand(&client.AppStop{}, "stop")

	return m
}

type coffeeceVersion struct{}

func (c *coffeeceVersion) Info() *cmd.Info {
	return &cmd.Info{Name: "version", Desc: "Exibe a versão do CLI.", OnlyAppendOnRoot: true}
}

func (c *coffeeceVersion) Run(ctx *cmd.Context) error {
	fmt.Fprintf(ctx.Stdout, "coffeece %s\n", version)
	return nil
}
