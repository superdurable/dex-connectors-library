# BigQuery connector for TypeScript

This package implements `listDatasets` with the shared [manifest](../connector.yaml) and Go-recorded provider scenarios. See the [connector guide](../README.md) for OAuth setup. The [runnable dataset Flow](examples/datasets/README.md) uses the static `google-bigquery` Dex Web connection and requires a project ID in its start input.

Run `npm ci` and `npm run check` from this directory. Live BigQuery authorization and Dex Web field rendering have not been verified.
