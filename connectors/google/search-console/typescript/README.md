# Google Search Console for Dex TypeScript

This package implements the `listSites` Query with a renewable OAuth connection and bounded provider requests. It returns property URLs and permission levels without account secrets. See the [connector guide](../README.md) for Google Cloud app setup.

The runnable [sites Flow](examples/sites/README.md) uses the static `google-search-console` Dex Web connection. From this package run `npm ci` and `npm run check`. Go-recorded provider scenarios replay in `test/conformance.test.ts`. The real Dex integration test uses a local provider fixture and synthetic token; live Google authorization remains unverified.
