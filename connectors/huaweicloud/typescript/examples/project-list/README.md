# Huawei Cloud IAM project list

1. Start Dex with `dexcli dev -open=false`.
2. In Dex Web Connections, create a Huawei Cloud connection named `huaweicloud`. Follow the authorization guide to configure an IAM user's account name, username, and password.
3. From `connectors/huaweicloud/typescript`, run `npm ci`, then `npm run example:worker`.
4. In another terminal, run `npm run example:start`.

The Flow in `flow.ts` persists project metadata only. The connector obtains an IAM token during the Query and keeps the password and token outside Dex state. The checked-in integration test uses a local provider fixture and a real Dex Worker and Client; real Huawei Cloud authorization has not been verified.

Generate strict FDG 2.0 from the installed example package:

```sh
npm run build
npm install --prefix examples/project-list --no-save --no-package-lock --ignore-scripts .
cd examples/project-list
dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```
