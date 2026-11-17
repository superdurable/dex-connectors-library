# Baidu Maps address lookup

The checked-in `BaiduMapsAddressLookupFlow` uses the static `baidu-maps` connection to geocode one Chinese address. It persists the BD-09 coordinate and completes. Optional branches are unwired, so a definite provider rejection fails the Flow. The example source contains the Flow, Worker entrypoint, and input sample.

Start `dexcli dev`, create the `baidu-maps` connection in Dex Web Connections with a server AK from [Baidu Maps Application Management](https://lbs.baidu.com/apiconsole/key), and set the project's `DEX_PROJECT_*` environment. From `connectors/baidu/maps/typescript`, run `npm ci` then `npm run example:address-lookup`. Start Flow `BaiduMapsAddressLookup` with a unique Flow ID and the content of [input.example.json](input.example.json). A local real Dex Worker/Client integration check runs with `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npm run test:integration` and needs no Baidu AK. It checks success, a 503 retry, and an unwired provider rejection.

For strict FDG 2.0 generation from this package directory:

```sh
npm run build
npm install --prefix examples/address-lookup --no-save --no-package-lock --ignore-scripts .
dexcli visualize examples/address-lookup/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/dex-baidu-maps-ts-fdg --open=false
```

The generated JSON must report `valid: true` with no diagnostics. The independent consumer package import ensures the generator reads this package's `dex-connector.json` contract. Provider authorization against a live Baidu Maps account is not covered by the local integration.
