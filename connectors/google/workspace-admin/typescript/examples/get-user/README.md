# Read a Workspace user

This runnable Flow reads one account with `getUser` and persists the bounded result. It uses the static Dex connection name `workspace-admin-directory`.

1. Configure that connection in Dex Web using the Google Workspace Admin authorization guide and either domain-wide delegation or Google OAuth.
2. Start `dexcli dev` and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address.
3. Run `npm run example:get-user:worker` in one shell.
4. Run `npm run example:get-user:start -- examples/get-user/input.example.json` in another shell.

The integration test uses a local Directory stand-in and a real Dex Worker and Client. Live Google authorization and provider behavior require a user-controlled connection.
