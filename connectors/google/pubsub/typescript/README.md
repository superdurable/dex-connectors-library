# PubSub connector for TypeScript

This package implements `listTopics` with the shared [manifest](../connector.yaml) and Go-recorded provider scenarios. See the [connector guide](../README.md) for OAuth setup. The [runnable topic Flow](examples/topics/README.md) uses the static `google-pubsub` Dex Web connection and requires a project ID in its start input.

Run `npm ci` and `npm run check` from this directory. Live PubSub authorization and Dex Web field rendering have not been verified.
