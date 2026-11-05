# MCP tool ↔ CLI command

| MCP tool | CLI |
|---|---|
| `goship_whoami` | `goship whoami` |
| `goship_list_orgs` | `goship orgs` |
| `goship_list_projects` | `goship projects` |
| `goship_list_apps` / `goship_get_app` | `goship apps` / `goship app info <name>` |
| `goship_create_app` | `goship app create <name> --platform go [--plan p] [--private]` |
| `goship_delete_app` | `goship app rm <name>` |
| `goship_start_app` / `_stop_app` / `_restart_app` | `goship app start\|stop\|restart <name>` |
| `goship_scale_app` | `goship app scale <name> --units N` |
| `goship_set_app_plan` | `goship app plan <name> <plan>` |
| `goship_set_app_visibility` | `goship app visibility <name> public\|private` |
| `goship_list_app_access` / `goship_add_app_access` / `goship_reset_app_access` / `goship_remove_app_access` | `goship app access list\|add\|reset\|rm <name> [user]` |
| `goship_get_app_access_token` (`reveal: true` for a usable token) / `goship_rotate_app_access_token` | `goship app access token <name> [--reveal\|--rotate]` |
| `goship_get_env` / `goship_set_env` / `goship_unset_env` | `goship env list\|set\|unset -a <app> [--no-restart]` |
| `goship_deploy` | `goship deploy [dir] [-a app] [--platform p] [--dockerfile f] [--image ref] [--plan p] [--env-file .env] [-m msg]` |
| `goship_list_releases` / `goship_rollback` | `goship releases -a <app>` / `goship rollback <version> -a <app>` |
| `goship_logs` | `goship logs -a <app> [-l N] [--source web]` |
| `goship_run` | `goship run -a <app> [--once] [--isolated] -- <cmd>` |
| `goship_list_databases` / `goship_get_database` / `goship_database_stats` | `goship dbs` / `goship db info <name>` / `goship db info <name>` (prints the stats too) |
| `goship_create_database` / `goship_delete_database` | `goship db create <name> --plan db-micro` (GoShip; `--server goship` or the tool's `server: goship` names GoShip's PostgreSQL outright) or `goship db create <name> --server s [--size-limit 2GB]` (own server, no plan) / `goship db rm <name>` |
| `goship_list_database_servers` / `goship_list_database_server_tiers` | `goship db server list` / `goship db server tiers` |
| `goship_create_database_server` / `goship_retry_database_server` / `goship_delete_database_server` | `goship db server create --account a --region r [--environment e] [--tier t] [--name n] [--disk-limit GB\|none]` / `goship db server retry <s>` / `goship db server delete <s> [--force]` |
| `goship_bind_database_server` / `goship_enable_database_server_pitr` | `goship db server bind <s> --environment e` / `goship db server pitr <s> [--yes]` |
| `goship_set_database_size_limit` / `goship_set_database_server_disk_limit` | `goship db size-limit <name> <size\|none>` / `goship db server disk-limit <s> <GB\|none>` |
| `goship_bind_database` / `goship_unbind_database` | `goship db bind\|unbind <name> -a <app>` |
| `goship_list_database_users` / `goship_create_database_user` | `goship db users <name>` / `goship db user-add <name> <user>` |
| `goship_list_database_extensions` / `goship_enable_database_extension` / `goship_disable_database_extension` | `goship db extensions <name>` / `goship db extension-add <name> <ext>` / `goship db extension-rm <name> <ext>` |
| `goship_list_domains` / `goship_register_domain` / `goship_verify_domain` / `goship_delete_domain` | `goship domains` / `goship domain register\|verify\|rm` |
| `goship_add_app_domain` / `goship_remove_app_domain` | `goship domain add\|rm <domain> -a <app>` |
| `goship_list_environments` | `goship environments` |
| `goship_enable_environment` / `goship_disable_environment` | `goship environment enable <slug>` / `goship environment disable <slug>` (staging starts disabled; refusals are printed as the API wrote them) |
| `goship_create_environment` / `goship_delete_environment` | `goship environment create <slug> [--name n] [--cluster c] [--db-server s]` / `goship environment rm <slug>` |
| `goship_list_clusters` / `goship_get_cluster` | `goship clusters` / `goship cluster info <cluster>` |
| `goship_create_cluster` | `goship cluster create <name> --cloud a --region r --size s`, or `--host ip [--ssh-user u] [--port p] --ssh-key path`, or `--kubeconfig path`, each with optional `[--environment slug]...` (the tool takes an `environments` array; it takes `ssh_key` and `kubeconfig` as contents: use the CLI for those) |
| `goship_list_cluster_sizes` / `goship_resize_cluster` | `goship cluster sizes <cluster>` / `goship cluster resize <cluster> --size <slug>` (on `reconnect_required`, send the user to the dashboard link in the error) |
| `goship_delete_cluster` | `goship cluster rm <cluster> [--destroy-server]` |
| `goship_list_volumes` / `goship_get_volume` / `goship_create_volume` / `goship_bind_volume` / `goship_unbind_volume` / `goship_delete_volume` | `goship volumes` / `goship volume info\|create\|bind\|unbind\|rm` (volumes need an environment on a cluster of the organization's own) |
| `goship_list_plans` | `goship plans [--kind app\|database]` |
| `goship_list_cloud_accounts` / `_regions` / `_sizes` / `goship_delete_cloud_account` | `goship cloud list\|regions\|sizes\|disconnect` |
| `goship_list_api_tokens` / `goship_create_api_token` / `goship_revoke_api_token` | `goship tokens` / `goship token create\|rm` |

Not tools, CLI only (they need a browser or a terminal): `goship login`,
`goship logout`, `goship cloud connect <provider>` (`--account <id>`
reconnects an API-key account in place, keeping its clusters), `goship shell`,
`goship environment use <slug>` (saved per organization and project; the local
MCP server reads it too).

Scope. Tools that act on an app, a database or a volume (also plans,
releases, logs, env and `goship_add_app_domain`) act in one environment and
take `org`, `project` and `environment`; the CLI takes `--org`, `--project`
and `--environment`. Environments, clusters, cloud accounts, database servers
and registered domains belong to the organization and take `org` only.
`goship apps\|dbs\|volumes -A` lists every environment of every project (no
tool does that: call the list tool once per environment from
`goship_list_environments`).

Every data command takes `--output json`; `-y` answers confirmations.
