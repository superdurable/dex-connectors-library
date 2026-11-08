# Google Analytics 4 for Dex TypeScript

This package implements the `runReport`, `getProperty`, and `listAudiences` Queries with a renewable Google OAuth connection. The checked-in [property Flow](examples/property/README.md) uses the static `google-analytics-4` Dex Web connection. Follow the [connector setup guide](../README.md) for enabled APIs, scopes, property access, and OAuth redirect configuration.

Run `npm ci`, `npm run check`, and `npm run test:integration` with a running `dexcli dev` and `DEX_FLOW_SERVICE_ADDRESS`. Go-recorded provider scenarios replay in `test/conformance.test.ts`. Google live authorization remains unverified.
