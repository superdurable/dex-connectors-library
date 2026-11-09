# LinkedIn Ads connector for TypeScript

This package implements `listAdAccounts` with the shared [manifest](../connector.yaml) and Go-recorded provider scenarios. See the [connector guide](../README.md) for OAuth setup. The [runnable account Flow](examples/accounts/README.md) uses the static `linkedin-ads` Dex Web connection and accepts an cursor-based page size and optional page token in its start input.

Run `npm ci` and `npm run check` from this directory. Live LinkedIn authorization and Dex Web field rendering have not been verified.
