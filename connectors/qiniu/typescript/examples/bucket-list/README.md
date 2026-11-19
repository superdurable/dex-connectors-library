# Qiniu Kodo bucket list

1. Start Dex with `dexcli dev -open=false`.
2. In Dex Web Connections, create a Qiniu connection named `qiniu`. Follow the authorization guide to create a paired AccessKey and SecretKey.
3. From `connectors/qiniu/typescript`, run `npm ci`, then `npm run example:worker`.
4. In another terminal, run `npm run example:start`.

The Flow in `flow.ts` persists bucket names only. The connector signs each read-only request and keeps SecretKey outside Dex state. The checked-in integration test uses a local provider fixture and a real Dex Worker and Client; real Qiniu authorization has not been verified.

Generate strict FDG 2.0 from the installed example package:

```sh
npm run build
npm install --prefix examples/bucket-list --no-save --no-package-lock --ignore-scripts .
cd examples/bucket-list
dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```
