# Review Sentry issues

`IssueReviewFlow` lists one issue page with the static `sentry-issue-review` connection. The connector Step makes the provider call and the Flow retains only the safe search input and issue metadata.

Start `dexcli dev` and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address. In Dex Web Connections, create `sentry-issue-review` with a Sentry auth token. Start at [Sentry Auth Tokens](https://sentry.io/settings/account/api/auth-tokens/) and follow **User Settings > Auth Tokens > Create New Token**. Grant `event:read` for this example. The optional status update operation also needs `event:write`. Set `DEX_CONNECTOR_CONFIG_FILE` to the mode-0600 local Dex Web `connections.json` file. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults.

From `connectors/sentry/typescript`, run the Worker:

```sh
npm run example:issue
```

In another terminal with the same configuration, edit `input.example.json` to use your organization slug and provider query, then start the Flow:

```sh
npm run example:issue:start -- examples/issue-review/input.example.json
```

The starter prints the returned issue page. Keep tokens out of the input file.

Run the real Dex integration test against a local Sentry HTTP fixture:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:27201 npm run test:integration
```

Generate and lint the strict Flow Definition Graph 2.0 with Dex CLI 1.28.0 or newer:

```sh
npm run build
npm install --prefix examples/issue-review --no-save --no-package-lock --ignore-scripts .
cd examples/issue-review
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

The example has no operation-specific Dex Web ConfigurationUI fields. Its visible setup fields are the manifest's token and optional base URL, timeout, and response limit, with guidance in the connector authorization guide. Real Sentry authorization and Dex Web Connections rendering remain unverified.
