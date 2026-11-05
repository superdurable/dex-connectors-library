# Google Workspace Admin Python account list

`AccountListFlow` reads one page of accounts with `new_list_users_step` and the static
`google-workspace-admin` connection. The `listed` branch completes with bounded
account metadata; unwired optional branches fail the Flow.

Configure the connection in Dex Web **Connections** using the
[Google Workspace Admin setup guide](../../../README.md). Domain delegation is
the default: enable the Admin SDK, authorize both Directory scopes for the
service account's client ID, add its JSON key, and name the delegated
administrator. Google OAuth is also supported. The connection's endpoint,
response size, and list page size have manifest defaults. This example has no
operation configuration fields or UI picker. Start the Flow with an optional
`domain` or `customer` plus search and pagination fields from `ListUsersInput`.

From the `python/` directory, generate the strict FDG 2.0 graph from this
runnable source using the package's interpreter:

```bash
mkdir -p build
dexcli visualize ./examples/account_lifecycle/flow.py --language python \
  --python .venv/bin/python --schema-version 2.0 --json \
  --out ./build/account-list-python
```

Run `dexcli dev` with the graph directory, load the Dex project connection, and
start the Worker:

```bash
uv run python -m examples.account_lifecycle.main
```

`DEX_FLOW_SERVICE_ADDRESS` selects a nondefault Dex server. Start
`GoogleWorkspaceAdminAccountListPython` with a unique Flow ID and a safe input,
for example `{"domain":"example.com","pageSize":100}`. A recent account may
not yet appear in Directory search results; use `getUser` for immediate read-back.

The real Dex Worker and Client integration test uses a local Directory stand-in
that binds an available port and verifies retry, completion, and unwired
provider rejection:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:42201 uv run \
  pytest examples/account_lifecycle/integration_test.py -q
```

Run `dexcli dev -dex-port 42201 -web-port 42202 -open=false` separately for this
command. The test never contacts a real Workspace tenant.
