# Microsoft Excel TypeScript example

The four Flows in the `*-flow.ts` files read a table, read a range, write literal range values, and append keyed table rows. All use the static `approvals` connection name. In Dex Web Connections, use the Microsoft Entra app registration and consent instructions in the [connector README](../../../README.md), then select a workbook through the connector's picker. The app registration needs delegated `Files.ReadWrite.All` and `offline_access`; Dex Web stores the tokens privately.

From `connectors/microsoft/excel/typescript`, start `dexcli dev` on an unused 40660–40689 port pair and set `DEX_FLOW_SERVICE_ADDRESS`. Start the Worker with `npm run example:worker`. In another terminal, start a Flow with `npm run example:start -- <ReadTable|ReadValues|WriteValues|AppendRows> <input.json>`. The JSON input uses the matching generated operation input fields, including `driveId` and `workbookId`. Do not put credentials in the input file.

For the checked in local Dex integration test, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:40660 npm run test:integration`. It uses inert credentials and a Graph fixture; live Microsoft OAuth and workbook behavior remain unverified.

Generate and lint each Flow Definition Graph 2.0 from this example directory after building the package and installing it locally:

```sh
npm run build --prefix ../..
npm install --no-save --no-package-lock --ignore-scripts ../..
for source in read-table-flow.ts read-values-flow.ts write-values-flow.ts append-rows-flow.ts; do
  dexcli visualize "$source" --language typescript --schema-version 2.0 --lint app --json --out - --open=false
done
```
