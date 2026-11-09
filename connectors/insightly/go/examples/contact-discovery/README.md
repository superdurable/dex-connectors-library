# Insightly contact discovery Flow

This runnable Flow calls `listContacts` with the static `insightly` Dex Web connection. It stores a bounded contact page in an Attribute and completes with the typed result. Start input `{"top":20,"skip":0}` reads the first page; increase `skip` to read another page. Optional branches are unwired and fail the Flow when selected. Credentials do not enter Flow state.

## Strict FDG 2.0 generation

From `connectors/insightly/go`, run:

```bash
dexcli visualize ./examples/contact-discovery/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/insightly-contact-discovery-fdg
```

The command must report `valid: true`. Before the first release, `connector_release_required` may warn that automatic Dex Web configuration requires a published release.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override insightly=<artifact-directory>`. In Dex Web Connections select `insightly / insightly`. Sign in at [Insightly CRM](https://login.insightly.com/), open the top-right user icon, then **User Settings > User Settings > API**. Save the full key in `api_key` and use the API URL shown there for `baseUrl` when your pod differs from na1.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/insightly/go`:

```bash
GOWORK=off go run ./examples/contact-discovery
```

Start `InsightlyContactDiscovery` with a unique Flow ID and input `{"top":20,"skip":0}`. The Worker has no inbound listener. The provider connection name is fixed in `flow/workflow.go`.

With `dexcli dev` serving at `127.0.0.1:8905`, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8905 GOWORK=off go test -race -tags=integration ./examples/contact-discovery/flow -count=1`. Live provider authorization remains unverified.
