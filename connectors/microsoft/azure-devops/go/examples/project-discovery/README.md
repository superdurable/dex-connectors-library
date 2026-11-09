# Azure DevOps project discovery Flow

This runnable Flow calls `listProjects` with the static `azure-devops` connection. Its successful result is saved in an Attribute and shown through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected. Only project IDs, names, and states enter the Flow result; the PAT does not.

## Strict FDG 2.0 generation

From `connectors/microsoft/azure-devops/go`, run:

```bash
dexcli visualize ./examples/project-discovery/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/azure-devops-project-discovery-fdg --open=false
```

The graph must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override azure-devops=<artifact-directory>`. In Dex Web Connections select `azure-devops / azure-devops`. Sign in at https://dev.azure.com/, open your organization, and copy its full URL into `baseUrl`. Under user settings > Personal access tokens > New Token, create an organization-scoped token with Project and Team (Read) and Work Items (Read) scopes. Copy the token shown once into the secret `personal_access_token` field. Blank `baseUrl` is invalid; blank timeout and response-size fields use their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/microsoft/azure-devops/go`:

```bash
GOWORK=off go run ./examples/project-discovery
```

Start `AzureDevOpsProjectDiscovery` with a unique Flow ID and input `{"top":20,"skip":0}`. Increase `skip` by the requested `top` size to read another page. The Worker connects to Dex Server without an inbound listener. The provider connection name is fixed in `flow/workflow.go`.

With `dexcli dev` serving at `127.0.0.1:8957`, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8957 GOWORK=off go test -race -tags=integration ./examples/project-discovery/flow -count=1`. Live provider authorization remains unverified.
