# Microsoft Outlook Calendar connector for TypeScript

This package implements `listEvents`, `getEvent`, `createEvent`, `updateEvent`, and `queryFreeBusy` for Dex. It pins `@superdurable/connectors-sdk` to `0.7.0` and uses the manifest-generated grant for delegated Microsoft OAuth or app-only token renewal. Configure the connection through Dex Web Connections, following the [authorization guide](../README.md#authorization). Never put tokens in Flow inputs.

The [runnable read-event Flow](examples/read-event) binds the static connection name `outlook-calendar-scheduler`. It exports a Worker entrypoint, a real Dex integration test with controlled Graph responses, and FDG 2.0 annotations. Run `npm ci`, `npm run check`, and `npm run test:integration` with `dexcli dev` on ports 40722 and 40723. Live Microsoft authorization has not been verified.
