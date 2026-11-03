# Resend TypeScript connector

`@superdurable/connector-resend` provides the `sendEmail` Mutation and `getEmail`
Query with the branches and defaults declared in `../connector.yaml`.

Create a named `resend` connection and add the operation-specific
`newSendEmailStep` or `newGetEmailStep` to a Dex Flow. See the runnable
[welcome email example](examples/welcome-email/README.md) for the complete
Step factory and Worker setup. Keep the API key in Dex Web Connections; the
Flow input contains only email content.

`npm run check` runs TypeScript, lint, provider replay, and unit tests.
`npm run integration:welcome` verifies the example against a local Dex stack.
