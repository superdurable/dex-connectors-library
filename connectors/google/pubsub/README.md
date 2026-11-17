# Google Cloud Pub/Sub connector

The Go connector provides `listTopics`, `getTopic`, `createTopic`, `updateTopicLabels`, and `deleteTopic`. The Python and TypeScript packages still provide `listTopics`. Topic creation, absolute label replacement, and deletion use stable resource names and safe repeat semantics. The runnable [Go](go/examples/topics/README.md), [Python](python/examples/topics/README.md), and [TypeScript](typescript/examples/topics/README.md) Flows use the static `google-pubsub` Dex Web connection.

In [Google Cloud Console](https://console.cloud.google.com/apis/credentials), select a project, enable Pub/Sub API, configure OAuth consent with the `pubsub` scope, and create a Web application client with the exact redirect URI displayed by Dex Web. Supply the project ID from Pub/Sub > Topics as operation input. The [topics.list API reference](https://docs.cloud.google.com/pubsub/docs/reference/rest/v1/projects.topics/list) defines the endpoint and pagination.

Live Google consent, token refresh, Pub/Sub reads, and Dex Web field rendering have not been verified in this checkout.
