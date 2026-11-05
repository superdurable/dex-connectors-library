# Get one GitLab project

This Flow reads a project through the static `gitlab` Dex Web connection. It persists the safe project request and result. The `found` branch completes the Flow; other provider branches fail when selected.

Configure the `gitlab` connection in Dex Web with a personal access token, then run the Worker with `npm run example:get-project`. In another shell, run `npm run example:get-project:start -- examples/get-project/input.example.json`. Set `DEX_FLOW_SERVICE_ADDRESS` for a Dex dev server at a nondefault address. `projectId` may be a numeric ID or namespace path.

Generate FDG 2.0 from this checked-in Worker source using the Dex CLI. Every Step has one `dex:explanation` beside its `dex:group`; keep those annotations with the Step when editing the Flow.
