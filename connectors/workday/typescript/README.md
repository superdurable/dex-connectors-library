# Workday connector for TypeScript

This package implements `getWorker` with the shared [manifest](../connector.yaml) and Go-recorded provider scenarios. See the [connector guide](../README.md) for OAuth setup and retry behavior. The [runnable worker Flow](examples/worker-inspection/README.md) uses the static `workday` Dex Web connection.

Run `npm ci` and `npm run check` from this directory. Live Workday authorization and Dex Web field rendering have not been verified.
