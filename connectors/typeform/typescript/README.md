# Typeform connector for Dex TypeScript

`@superdurable/connector-typeform` provides `listForms`, `getForm`, `listResponses`, and `upsertWebhook` operations for Dex Flows. The `responseSubmitted` Trigger remains Go only. Each operation uses a Typeform personal access token; only `upsertWebhook` needs `webhook_secret`. A missing `webhook_secret` does not prevent the three read operations.

Declare a Typeform connection named `typeform-forms` in Dex Web. Create a personal token at <https://admin.typeform.com/user/tokens> with `forms:read`, `responses:read`, and `webhooks:write` scopes. For webhook registration, generate a separate signing secret and place it in the optional `webhook_secret` field. Keep both values in the connection, never in Flow input.

See [the runnable list forms example](examples/list-forms/README.md) for the Worker, starter, and Dex integration test. Its source contains the application API and static `ConnectionName`. `getForm` and `listResponses` return typed form and answer data. `upsertWebhook` uses a stable PUT by form ID and tag, so Dex may retry it safely; the echoed signing secret is discarded.

`dataCenter` selects the Typeform API host (`us`, `eu`, or `newEu`). `maxResponseBytes` bounds successful API bodies; a larger response selects `invalidResponse`. Provider 408, 429, 5xx, and transport failures retry through Dex. A provider rejection takes the named optional branch, which fails the Flow if the application leaves it unwired.

Run `npm run check` for package tests, types, and lint. Run `make test-connector-conformance CONNECTOR_DIRECTORY=connectors/typeform` from the repository root to replay Go recordings. Live Typeform authorization and API behavior have not been verified.
