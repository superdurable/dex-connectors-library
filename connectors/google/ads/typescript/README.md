# Ads connector for TypeScript

This package implements `listAccessibleCustomers` with the shared [manifest](../connector.yaml) and Go-recorded provider scenarios. See the [connector guide](../README.md) for OAuth setup. The [runnable customer Flow](examples/customers/README.md) uses the static `google-ads` Dex Web connection and accepts an optional local maximum customer count in its start input.

Run `npm ci` and `npm run check` from this directory. Live Ads authorization and Dex Web field rendering have not been verified.
