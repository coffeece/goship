---
description: Deploy the current directory (or a given path) to GoShip
argument-hint: [path]
---

Deploy `$ARGUMENTS` (the current directory when empty) to GoShip by following
the `goship-deploy` skill end to end: read the project and report what you
found, make only the changes GoShip needs, write `goship.yaml` and
`.goshipignore` if missing, deploy with the `goship_deploy` tool (or
`goship deploy` when the MCP server is not available), then verify the URL
answers and report it.
