# SurveyMonkey TypeScript survey inspection

This Flow calls `listSurveys` with the static `surveymonkey` Dex Web connection. It stores the survey page in a Dex Attribute and completes with the typed result. Configure the Access Token from https://developer.surveymonkey.com > My Apps > the app > Settings in Dex Web Connections. Set the API v3 base URL for EU or Canadian accounts; blank uses the displayed US default.

With `DEX_PROJECT_*` and `DEX_FLOW_SERVICE_ADDRESS` set for a running Dex development environment, run from this package:

```sh
npm ci
npm run example:worker
```

In another terminal with the same project configuration, start a page:

```sh
npm run example:start -- 1
```

The Worker has no inbound listener. Optional outcome branches are unwired and fail the Flow when selected. For deterministic real-Dex verification, run `npm run test:integration` against `dexcli dev`. Live SurveyMonkey authorization has not been tested.

## Strict FDG 2.0 generation

After building the package, make its own package import available to the analyzer:

```sh
npm run build
npm install --prefix examples/survey-inspection --no-save --no-package-lock --ignore-scripts .
cd examples/survey-inspection
dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/surveymonkey-ts-fdg --open=false
```

The generated FDG must report `valid: true` with no diagnostics.
