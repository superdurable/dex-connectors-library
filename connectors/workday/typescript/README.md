# Workday connector for TypeScript

This package implements `getWorker` and `listWorkers` with the shared [manifest](../connector.yaml) and Go-recorded provider scenarios. See the [connector guide](../README.md) for OAuth setup and retry behavior. The [runnable worker Flow](examples/worker-inspection/README.md) uses the static `workday` Dex Web connection.

Run `npm ci` and `npm run check` from this directory. Live Workday authorization and Dex Web field rendering have not been verified.

Version 0.2.0 adds worker search and pagination and uses Connector SDK 0.8.0 to enforce the declared OAuth exchange timeout. Applications that bind custom operations must supply both worker queries. Projects that construct a connection from the provider client or Dex Web settings receive both automatically.
