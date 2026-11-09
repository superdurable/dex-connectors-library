# MicrosoftAdvertising connector for TypeScript

This package implements `listCampaigns` with the shared [manifest](../connector.yaml) and Go-recorded provider scenarios. See the [connector guide](../README.md) for OAuth setup and retry behavior. The [runnable worker Flow](examples/campaign-discovery/README.md) uses the static `microsoft-advertising` Dex Web connection.

Run `npm ci` and `npm run check` from this directory. Live MicrosoftAdvertising authorization and Dex Web field rendering have not been verified.
