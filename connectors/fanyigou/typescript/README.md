# Fanyigou TypeScript connector

Read-only `getAccountBalance` for Dex TypeScript Flows. The [runnable example](examples/basic/) uses a real Dex Worker and Client. Configure `fanyigou` in Dex Web Connections using the [provider guide](../README.md). Go-recorded provider scenarios are replayed in the package tests. Live provider authorization remains unverified.

The TypeScript `balance` field uses a JavaScript number. Counts above `Number.MAX_SAFE_INTEGER` select `invalidResponse` to prevent rounding.
