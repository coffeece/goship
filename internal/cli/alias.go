package cli

// legacyNames maps the v0.1.0 command names onto their replacements. They are
// rewritten before cobra sees them rather than registered as hidden commands,
// so they keep working without appearing anywhere in help.
var legacyNames = map[string][]string{
	"app-list":            {"apps"},
	"list":                {"apps"},
	"app-info":            {"app", "info"},
	"info":                {"app", "info"},
	"app-create":          {"app", "create"},
	"app-remove":          {"app", "rm"},
	"app-start":           {"app", "start"},
	"start":               {"app", "start"},
	"app-stop":            {"app", "stop"},
	"stop":                {"app", "stop"},
	"app-restart":         {"app", "restart"},
	"restart":             {"app", "restart"},
	"app-log":             {"logs"},
	"log":                 {"logs"},
	"app-run":             {"run"},
	"app-deploy-list":     {"releases"},
	"app-deploy-rollback": {"rollback"},
	"env-get":             {"env", "list"},
	"env-set":             {"env", "set"},
	"env-unset":           {"env", "unset"},
	"volume-create":       {"volume", "create"},
	"volume-list":         {"volume", "list"},
	"volume-info":         {"volume", "info"},
	"volume-bind":         {"volume", "bind"},
	"volume-unbind":       {"volume", "unbind"},
	"volume-delete":       {"volume", "rm"},
	"cname-add":           {"domain", "add"},
	"cname-remove":        {"domain", "rm"},
	"user-info":           {"whoami"},
}

// Rewrite maps a legacy command name onto its replacement. Everything else is
// returned untouched.
func Rewrite(args []string) []string {
	if len(args) == 0 {
		return args
	}
	replacement, ok := legacyNames[args[0]]
	if !ok {
		return args
	}
	return append(append([]string{}, replacement...), args[1:]...)
}
