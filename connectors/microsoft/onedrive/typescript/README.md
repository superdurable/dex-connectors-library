# Microsoft OneDrive TypeScript connector

This package implements `searchFiles`, `getFile`, `readFileText`, `uploadFile`, and `createFolder` with the generated Dex Connector SDK 0.7.0 OAuth and app-only credential refresh. Use the operation-specific Step factories exported from the package. The [get-file example](examples/get-file/README.md) is a runnable Flow with the static `onedrive-files` connection.

The public configuration and authorization instructions are in the [connector README](../README.md). No bearer token, refresh token, client secret, or preauthenticated download URL belongs in Flow input or persisted output.

Run `npm run check` for static checks and Go-recorded provider conformance. With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `npm run test:integration` to exercise a real Worker, Client, and persistence boundary. Live Microsoft authorization has not been exercised.
