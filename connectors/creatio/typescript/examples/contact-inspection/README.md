# Creatio TypeScript contact inspection

This runnable Flow calls `getContact` through the static `creatio` connection and persists the contact profile. Optional branches are unwired and fail the Flow when selected. Configure `client_id` and secret `client_secret` using the [connection guide](../../../README.md); enter `baseUrl` and `tokenEndpoint`; leave `access_token` blank for automatic renewal.

From `connectors/creatio/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- 90a68db5-cb3a-4f64-bb60-c0a934ee7600`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service.

Generate and lint the Flow Definition Graph 2.0 from the same directory:

```bash
mkdir -p node_modules/@superdurable
ln -s ../.. node_modules/@superdurable/connector-creatio
dexcli visualize examples/contact-inspection/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/creatio-typescript-fdg --open=false
```
