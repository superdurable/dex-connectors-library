# Customer issue search, TypeScript

`CustomerIssueFlow` starts at `SearchCustomerIssueTickets` and returns one Zendesk export-search page from the happy `searched` branch. The static Dex Web connection name is `zendesk-support`, defined in [flow.ts](flow.ts). The start input is a `SearchTicketsInput` such as `{ "statuses": ["open"], "requesterEmail": "jane@example.com" }`.

Configure the connection in Dex Web with `subdomain`, agent `email`, and secret `api_token`, then start `dexcli dev`. From `connectors/zendesk/support/typescript`, run `npm run example:customer-issue`. `main.ts` loads project configuration and starts the Worker. Set `DEX_FLOW_SERVICE_ADDRESS` when Dex uses a different address.

Run `npm run integration:customer-issue` with the same Dex server for a credential-free integration check. It exercises success, retry, and an unwired provider rejection through a real Dex Worker and Client. Live Zendesk behavior remains unverified.

For strict FDG 2.0 generation from this example, run:

```sh
dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/zendesk-customer-issue
```

With the current Dex CLI, strict visualization writes partial JSON and reports
`dynamic_class` for the generated Step factory's `searched` target and
`dynamic_step_target` for its registration. The Flow passes the real Worker and
Client integration check; those analyzer diagnostics remain unverified until
the CLI supports this generated TypeScript factory shape.
