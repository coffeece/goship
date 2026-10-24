# MCP tool ↔ CLI command

| MCP tool | CLI |
|---|---|
| `goship_whoami` | `goship whoami` |
| `goship_list_orgs` | `goship orgs` |
| `goship_list_projects` | `goship projects` |
| `goship_list_apps` / `goship_get_app` | `goship apps` / `goship app info <name>` |
| `goship_create_app` | `goship app create <name> --platform go [--plan p] [--node n] [--private]` |
| `goship_delete_app` | `goship app rm <name>` |
| `goship_start_app` / `_stop_app` / `_restart_app` | `goship app start\|stop\|restart <name>` |
| `goship_scale_app` | `goship app scale <name> --units N` |
| `goship_set_app_plan` | `goship app plan <name> <plan>` |
| `goship_set_app_visibility` | `goship app visibility <name> public\|private` |
| `goship_list_app_access` / `goship_add_app_access` / `goship_reset_app_access` / `goship_remove_app_access` | `goship app access list\|add\|reset\|rm <name> [user]` |
| `goship_get_app_access_token` (`reveal: true` for a usable token) / `goship_rotate_app_access_token` | `goship app access token <name> [--reveal\|--rotate]` |
| `goship_get_env` / `goship_set_env` / `goship_unset_env` | `goship env list\|set\|unset -a <app> [--no-restart]` |
| `goship_deploy` | `goship deploy [dir] [-a app] [--platform p] [--dockerfile f] [--plan p] [--node n] [--env-file .env] [-m msg]` |
| `goship_list_releases` / `goship_rollback` | `goship releases -a <app>` / `goship rollback <version> -a <app>` |
| `goship_logs` | `goship logs -a <app> [-l N] [--source web]` |
| `goship_run` | `goship run -a <app> [--once] [--isolated] -- <cmd>` |
| `goship_list_databases` / `goship_get_database` / `goship_database_stats` | `goship dbs` / `goship db info <name>` / `goship db stats <name>` |
| `goship_create_database` / `goship_delete_database` | `goship db create <name> --plan db-micro` (GoShip) or `goship db create <name> --server s [--size-limit 2GB]` (own server, no plan) / `goship db rm <name>` |
| `goship_list_database_servers` / `goship_list_database_server_tiers` | `goship db server list` / `goship db server tiers` |
| `goship_create_database_server` / `goship_retry_database_server` / `goship_delete_database_server` | `goship db server create --account a --region r [--tier t] [--name n] [--disk-limit GB\|none]` / `goship db server retry <s>` / `goship db server delete <s> [--force]` |
| `goship_set_database_size_limit` / `goship_set_database_server_disk_limit` | `goship db size-limit <name> <size\|none>` / `goship db server disk-limit <s> <GB\|none>` |
| `goship_bind_database` / `goship_unbind_database` | `goship db bind\|unbind <name> -a <app>` |
| `goship_list_database_users` / `goship_create_database_user` | `goship db users <name>` / `goship db user-add <name> <user>` |
| `goship_list_database_extensions` / `goship_enable_database_extension` / `goship_disable_database_extension` | `goship db extensions <name>` / `goship db extension-add <name> <ext>` / `goship db extension-rm <name> <ext>` |
| `goship_list_domains` / `goship_register_domain` / `goship_verify_domain` / `goship_delete_domain` | `goship domains` / `goship domain register\|verify\|rm` |
| `goship_add_app_domain` / `goship_remove_app_domain` | `goship domain add\|rm <domain> -a <app>` |
| `goship_list_nodes` / `goship_get_node` / `goship_create_node` / `goship_delete_node` | `goship nodes` / `goship node info\|create\|rm` |
| `goship_list_node_sizes` / `goship_resize_node` | `goship node sizes <node>` / `goship node resize <node> --size <slug>` |
| `goship_list_volumes` … `goship_delete_volume` | `goship volumes` / `goship volume create\|bind\|unbind\|rm` |
| `goship_list_plans` | `goship plans [--kind database\|volume] [--node n]` |
| `goship_list_cloud_accounts` / `_regions` / `_sizes` / `goship_delete_cloud_account` | `goship cloud list\|regions\|sizes\|disconnect` |
| `goship_move_cloud_account` | `goship cloud move <account> --to-project <slug>\|--to-org` |
| `goship_list_api_tokens` / `goship_create_api_token` / `goship_revoke_api_token` | `goship tokens` / `goship token create\|revoke` |

Not tools, CLI only (they need a browser or a terminal): `goship login`,
`goship logout`, `goship cloud connect <provider>`, `goship shell`.

Every data command takes `--output json`; `-y` answers confirmations.
