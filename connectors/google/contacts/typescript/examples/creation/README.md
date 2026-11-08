# Google Contacts creation Flow (TypeScript)

This runnable Flow creates one contact through the static `google-contacts` connection. Start it with a nonempty `givenName` and optional `familyName` and `emailAddress`. The confirmed result contains a `people/{id}` resource name.

From this package, link the unreleased package under `node_modules/@superdurable/connector-google-contacts` and run `dexcli visualize examples/creation/flow.ts --schema-version 2.0 --lint app --json --out /tmp/google-contacts-creation-typescript-fdg --open=false` for FDG 2.0. With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801` and run `npm run test:integration` for a real Worker and Client test with the generated mock. After saving a Google OAuth connection in Dex Web, run `npm run example:create:worker` and `npm run example:create:start`. Live Google authorization has not been verified here.
