# GitHub profile Python example

From `connectors/github/python`, generate the strict FDG 2.0 artifact:

```sh
uv sync
dexcli visualize ./examples/profile/flow.py --language python \
  --python .venv/bin/python --schema-version 2.0 --json \
  --out ./build/github-profile
```

The output must have `"valid": true` and no blocking diagnostics. Start
`dexcli dev -flow-rendering-dir ./build`, then set up the `github-profile`
OAuth connection in Dex Web. Its authorization guide lists the GitHub OAuth
App, callback URL, and requested scopes. Run
`uv run python -m examples.profile.main` from `connectors/github/python`.
Start `GitHubAuthenticatedProfilePython` with
`{"requested_by":"operator"}`. The Flow stores the verified profile in the
`github-profile-result` Attribute and completes with the same value.

The integration test uses an inert token and a local GitHub stand-in. Run it
with `DEX_FLOW_SERVICE_ADDRESS=<dexcli-address> uv run pytest -q examples/profile/integration_test.py`.
