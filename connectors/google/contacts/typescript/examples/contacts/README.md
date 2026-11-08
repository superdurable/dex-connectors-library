# Google Contacts list Flow (TypeScript)

This runnable Flow lists one contact page through the static `google-contacts` connection. From this package run `npm ci` and `npm run check`. For local FDG 2.0 generation, link this unreleased package under `node_modules/@superdurable/connector-google-contacts`, then run `dexcli visualize examples/contacts/flow.ts --schema-version 2.0 --lint app --json --out /tmp/google-contacts-typescript-fdg --open=false`.

With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801` and run `npm run test:integration` to verify the provider retry through a real Worker and Client. After saving a Google OAuth connection in Dex Web, run `npm run example:worker` and `npm run example:start`. Live Google authorization has not been verified here.
