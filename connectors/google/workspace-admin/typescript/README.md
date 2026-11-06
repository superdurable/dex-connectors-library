# Google Workspace Admin TypeScript connector

`@superdurable/connector-google-workspace-admin` implements seven Dex Step operations against Google's Admin SDK Directory API: `getUser`, `listUsers`, `createUser`, `suspendUser`, `unsuspendUser`, `addUserToGroup`, and `removeUserFromGroup`. It uses the manifest-generated Step factories and OAuth refresh declaration. `createUser` writes a stable external creation key and reads back conflicts; group addition and removal likewise converge after retries.

The [get-user example](examples/get-user/README.md) is a runnable Dex Flow with the static connection name `workspace-admin-directory`. Bind that name in Dex Web using the connector's authorization guide. Domain-wide delegation and interactive Google OAuth are both supported through the generated credential provider and refresh driver. The package never asks for provider tokens in Flow input.

For the complete runnable import and Flow definition, see `examples/get-user/flow/workflow.ts`. Operation errors and rejected credentials select optional failure branches; wire an optional branch only when the Flow has a business reaction for it. The default example leaves them unwired, so Dex fails the Flow on those outcomes.

Run `npm ci`, `npm run check`, and `npm run test:integration` with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev`. Provider conformance replays Go-recorded HTTP scenarios. Live Google authorization, scopes, and Directory behavior need a user-controlled connection and were not verified by replay.
