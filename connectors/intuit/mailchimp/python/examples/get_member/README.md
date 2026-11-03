# Mailchimp contact read Flow

This runnable Python Flow reads one Mailchimp audience contact through the static
`mailchimp-audience` connection. The returned contact is persisted as a safe
Attribute. Only `found` is wired; other definite outcomes fail the Flow.
The sample input and integration test exercise an internationalized email address.

From `connectors/intuit/mailchimp`, create a strict FDG 2.0 rendering for the
existing Go application setup, which exposes the same connection form:

```sh
mkdir -p build
 dexcli visualize ./go/examples/approved-campaign-send/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/approved-campaign-send
```

Start `dexcli dev -dex-port 25401 -web-port 25402 -flow-rendering-dir ./build/approved-campaign-send`, configure
`mailchimp / mailchimp-audience` in Dex Web **Connections**, and point
`DEX_CONNECTOR_CONFIG_FILE` to its owner-only `connections.json`. The key stays in
that private file and is resolved again for every provider request.

From `connectors/intuit/mailchimp/python`:

```sh
uv sync --locked
DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json uv run python -m examples.get_member.main
DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json uv run python -m examples.get_member.start \
  mailchimp-contact-001 examples/get_member/sample-input.json
```

With a local `dexcli dev -dex-port 25401 -web-port 25402` running, verify
Worker, Client, retry, malformed response routing, address rejection, and
optional branch routing against a local provider stand-in:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:25401 uv run pytest examples/get_member/integration_test.py
```
