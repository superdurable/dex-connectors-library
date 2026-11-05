# Jira Service Management connector for Dex Python

`dex-connector-atlassian-jira-service-management==0.1.0` provides seven typed Jira Service Management operations: `findCustomerByEmail`, `searchTickets`, `getTicket`, `createTicket`, `updateTicket`, `transitionTicket`, and `addComment`. It requires `dex-connectors-sdk==0.5.0`. The manifest defines the OAuth scopes and refresh policy; the generated project connection refreshes access tokens and persists rotated refresh tokens.

The [customer lookup example](examples/customer_lookup/README.md) runs a Flow through a static `jsm-support` connection. Start `dexcli dev`, configure the Atlassian OAuth connection in Dex Web, and run `python -m examples.customer_lookup.main`. The example's integration test uses a real Dex Worker and Client and a local provider stand-in. Provider conformance fixtures replay the Go connector's decisions across every operation.

Live Atlassian consent, site selection, and provider calls have not been verified. See the [connector README](../README.md) for the provider application setup and operation contracts.
