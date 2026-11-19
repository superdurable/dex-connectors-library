# Youzan shop lookup

1. Start Dex with `dexcli dev -open=false`.
2. In Dex Web Connections, create a Youzan connection named `youzan`. Follow the authorization guide to create a shop access token.
3. From `connectors/youzan/typescript`, run `npm ci`, then `npm run example:worker`.
4. In another terminal, run `npm run example:start`.

The Flow in `flow.ts` persists only shop metadata. The provider lookup executes in the connector Step. An unwired optional branch fails the Flow. The checked-in integration test uses a local provider fixture and a real Dex Worker and Client; real Youzan authorization has not been verified.

Generate strict FDG 2.0 from the installed example package:

```sh
npm run build
npm install --prefix examples/shop-basic --no-save --no-package-lock --ignore-scripts .
cd examples/shop-basic
dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```
