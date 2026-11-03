# Zendesk Support for Dex TypeScript

Install `@superdurable/connector-zendesk-support` and bind a static `zendesk-support` connection. The package implements `searchTickets`, `getTicket`, `createTicket`, and `updateTicket`; it has no Triggers. Operation-specific Step factories, models, definitions, and scripted mocks are generated from the connector manifest and Go models.

The runnable [customer-issue example](examples/customer-issue/README.md) searches one ticket page. Its Worker uses `newProjectConnection` to load the Zendesk subdomain and credential fields from Dex project configuration. Enter `email` and `api_token` in Dex Web Connections; the token stays in the credential store. All optional error branches remain unwired in the example, so they fail the Flow with their safe Failure kind.

Run `npm ci`, then `npm run check` from this directory. To verify the example through a real Dex Worker and Client, start `dexcli dev` and run `npm run integration:customer-issue`. The 42 checked-in Go recordings replay every operation's branches and retry classifications in `npm test`. Live Zendesk authorization and API behavior have not been verified.

Regenerate from the repository root with `GOWORK=off go run ./cmd/connectorctl generate connectors/zendesk/support/connector.yaml`, then check drift with `GOWORK=off go run ./cmd/connectorctl generate --check connectors/zendesk/support/connector.yaml`.
