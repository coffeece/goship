# MCP tool ↔ CLI command

| MCP tool | CLI |
|---|---|
| `goship_whoami` | `goship whoami` |
| `goship_list_orgs` | `goship orgs` |
| `goship_list_apps` / `goship_get_app` | `goship apps` / `goship app info <name>` |
| `goship_create_app` | `goship app create <name> --platform go [--plan p] [--node n]` |
| `goship_delete_app` | `goship app rm <name>` |
| `goship_start_app` / `_stop_app` / `_restart_app` | `goship app start\|stop\|restart <name>` |
| `goship_scale_app` | `goship app scale <name> --units N` |
| `goship_set_app_plan` | `goship app plan <name> <plan>` |
| `goship_get_env` / `goship_set_env` / `goship_unset_env` | `goship env list\|set\|unset -a <app> [--no-restart]` |
| `goship_deploy` | `goship deploy [dir] [-a app] [--platform p] [--dockerfile f] [--plan p] [--node n] [--env-file .env] [-m msg]` |
| `goship_list_releases` / `goship_rollback` | `goship releases -a <app>` / `goship rollback <version> -a <app>` |
| `goship_logs` | `goship logs -a <app> [-l N] [--source web]` |
| `goship_run` | `goship run -a <app> [--once] [--isolated] -- <cmd>` |
| `goship_list_databases` / `goship_get_database` / `goship_database_stats` | `goship dbs` / `goship db info <name>` / `goship db stats <name>` |
| `goship_create_database` / `goship_delete_database` | `goship db create <name> --plan db-small` / `goship db rm <name>` |
| `goship_bind_database` / `goship_unbind_database` | `goship db bind\|unbind <name> -a <app>` |
| `goship_list_database_users` / `goship_create_database_user` | `goship db users <name>` / `goship db user-add <name> <user>` |
| `goship_list_domains` / `goship_register_domain` / `goship_verify_domain` / `goship_delete_domain` | `goship domains` / `goship domain register\|verify\|rm` |
| `goship_add_app_domain` / `goship_remove_app_domain` | `goship domain add\|rm <domain> -a <app>` |
| `goship_list_nodes` / `goship_get_node` / `goship_create_node` / `goship_delete_node` | `goship nodes` / `goship node info\|create\|rm` |
| `goship_list_volumes` … `goship_delete_volume` | `goship volumes` / `goship volume create\|bind\|unbind\|rm` |
| `goship_list_plans` | `goship plans [--kind database\|volume] [--node n]` |
| `goship_list_cloud_accounts` / `_regions` / `_sizes` / `goship_delete_cloud_account` | `goship cloud list\|regions\|sizes\|rm` |
| `goship_list_api_tokens` / `goship_create_api_token` / `goship_revoke_api_token` | `goship tokens` / `goship token create\|revoke` |

Not tools, CLI only (they need a browser or a terminal): `goship login`,
`goship logout`, `goship cloud connect <provider>`, `goship shell`.

Every data command takes `--output json`; `-y` answers confirmations.
