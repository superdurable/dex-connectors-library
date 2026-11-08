# Firestore connector for TypeScript

This package implements `listDocuments` with the shared [manifest](../connector.yaml) and Go-recorded provider scenarios. See the [connector guide](../README.md) for OAuth setup. The [runnable document Flow](examples/documents/README.md) uses the static `google-firestore` Dex Web connection and requires project and top-level collection IDs in its start input.

Run `npm ci` and `npm run check` from this directory. Live Firestore authorization and Dex Web field rendering have not been verified.
