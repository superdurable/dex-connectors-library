# Read one OneDrive file

`GetFileFlow` uses the static `onedrive-files` connection to return file or folder metadata. Configure that connection in Dex Web using the [OneDrive authorization guide](../../../README.md). Its `found` branch completes the Flow; optional error branches fail it. Credentials never enter the Flow input or output.

Start a Dex development stack, set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address and `DEX_CONNECTOR_CONFIG_FILE` to the private project connection file. From this package directory, run `npm run example:worker`, then in another terminal run `npm run example:start -- examples/get-file/input.example.json`. Set `itemId` to a real Graph item ID; `root` reads the drive root. Set `driveId` for another drive or an app-only connection.

Run `DEX_FLOW_SERVICE_ADDRESS=<dex-address> npm run test:integration` for a real Dex Worker and Client test with a local Graph stand-in. Generate strict Flow Definition Graph 2.0 with Dex CLI 1.28.0 or newer:

```sh
npm run build
npm install --prefix examples/get-file --no-save --no-package-lock --ignore-scripts .
cd examples/get-file
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Live Microsoft authorization has not been exercised.
