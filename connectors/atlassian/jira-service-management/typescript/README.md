# Jira Service Management TypeScript connector

Install `@superdurable/connector-atlassian-jira-service-management@0.1.0` with `@superdurable/connectors-sdk@0.5.0`. This package implements `findCustomerByEmail`, `searchTickets`, `getTicket`, `createTicket`, `updateTicket`, `transitionTicket`, and `addComment`; it has no Triggers. Operation-specific Step factories, connection types, models, and mock cases are generated from `connector.yaml`.

The runnable [find-customer example](examples/find-customer/README.md) supplies a Worker entrypoint and a Flow using the static `jira-service-management-support` connection. Its integration test uses a real Dex Worker and Client against a local Atlassian stand-in. The conformance suite replays Go-recorded provider scenarios, including retries, failures, and uncertain writes.

Create an Atlassian OAuth 2.0 (3LO) app and configure Dex Web Connections using the [connector setup guide](../README.md#atlassian-setup). The generated refresh declaration uses Atlassian's JSON token request, replaces the rotating refresh token, refreshes stored tokens with missing expiry, and requires the five Jira API scopes if Atlassian returns a scope string. Credentials remain in the host-owned connection source.

From this directory, run `npm ci`, `npm run check`, and `npm run test:integration` with `DEX_FLOW_SERVICE_ADDRESS` set to a running `dexcli dev` instance. The live Atlassian OAuth flow and API behavior have not been verified for this TypeScript package.
