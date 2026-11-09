# Constant Contact TypeScript contact inspection

This runnable Flow calls `getContact` through the static `constantcontact` connection, persists the result, and completes. Create a V3 OAuth application in Constant Contact My Applications, register the Dex Web redirect URI, and authorize `contact_data` and `offline_access`.

From `connectors/constantcontact/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- <contact-uuid>`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Optional branches are unwired and fail the Flow when selected.

For strict FDG 2.0 verification, build the package and make its own package import visible to the analyzer:

```bash
npm run build
npm install --prefix examples/contact-inspection --no-save --no-package-lock --ignore-scripts .
dexcli visualize examples/contact-inspection/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/constantcontact-ts-fdg --open=false
```

The generated FDG must report `valid: true` with no diagnostics.
