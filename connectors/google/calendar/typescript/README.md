# Google Calendar TypeScript connector

Install `@superdurable/connector-google-calendar@0.1.0` with `@superdurable/connectors-sdk@0.5.0`. The package exposes `newListEventsStep`, `newGetEventStep`, `newCreateEventStep`, `newUpdateEventStep`, and `newQueryFreeBusyStep`, plus typed inputs, outputs, mock outcomes, and `newProjectConnection`.

The connector uses the static Dex Web connection name `google-calendar` in its [runnable meeting example](examples/schedule-meeting/README.md). The example reads an event through a real Dex Worker. Other operations use the same connection. `createEvent` writes under a stable event ID, and `updateEvent` reads before patching so retries do not repeat an already applied write.

Google OAuth and Workspace domain-wide delegation use the manifest-generated refresh driver and private/shared credential decoders. The project connection keeps credentials in host-owned storage and refreshes them before use. Set up the Google Cloud project, Calendar API, scopes, redirect URI, and consent screen using the [connector authorization guide](../README.md#authorization). Keep secrets out of Flow inputs and results.

Run `npm ci`, `npm run check`, and `npm run test:integration` from this directory. The integration test requires `DEX_FLOW_SERVICE_ADDRESS` for a running `dexcli dev` server and binds its Google stand-in to an OS-assigned port. Live Google authorization, delegated JWT minting against Google, Calendar API responses, and Dex Web Connections were not verified.
