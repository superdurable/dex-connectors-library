# Google Tag Manager for Dex TypeScript

This package implements `listAccounts`, `listContainers`, and `listTags` with a renewable Google OAuth connection. The checked-in [accounts Flow](examples/accounts/README.md) uses the static `google-tag-manager` Dex Web connection. Follow the [connector setup guide](../README.md) for enabled APIs, scope, account access, and OAuth redirect configuration.

Run `npm ci`, `npm run check`, and `npm run test:integration` with a running `dexcli dev` and `DEX_FLOW_SERVICE_ADDRESS`. Go-recorded provider scenarios replay in `test/conformance.test.ts`. Google live authorization remains unverified.
