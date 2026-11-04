# SendGrid statistics Flow

This runnable Flow reads SendGrid global email statistics through the static `sendgrid` connection. It persists safe request and result fields, then completes on `found`. Optional failure branches are unwired and fail the Flow. The integration test runs this same Flow through a real Dex Worker and Client with a local provider stand-in.

## Start

In Dex Web Connections, select SendGrid and use the guide at https://app.sendgrid.com/settings/api_keys to create or select a read-enabled API key. Save it in the secret `api_key` field; leave the base URL and safety limits blank for their defaults. With the project environment configured and `dexcli dev` running, start the Worker with `npm run example:stats` and start a Flow with `npm run example:stats:start -- examples/stats/input.example.json`. Set `DEX_FLOW_SERVICE_ADDRESS` if Dex is not at its default local address.

## Strict FDG 2.0 generation

From the package root, run `dexcli visualize ./examples/stats/flow/workflow.ts --schema-version 2.0 --json --out ./build/stats`. The command must report `valid: true`; an unpublished connector may require a local release override.

The provider key, live authorization, and Dex Web form have not been verified against SendGrid.
