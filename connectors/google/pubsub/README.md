# Google Cloud Pub/Sub connector

The Go connector provides `listTopics`, `getTopic`, `createTopic`, `updateTopicLabels`, and `deleteTopic`. The Python and TypeScript packages still provide `listTopics`. Topic creation, absolute label replacement, and deletion use stable resource names and safe repeat semantics. The runnable [Go](go/examples/topics/README.md), [Python](python/examples/topics/README.md), and [TypeScript](typescript/examples/topics/README.md) Flows use the static `google-pubsub` Dex Web connection.

Go also provides `listSubscriptions`, `getSubscription`, `createSubscription`, `updateSubscriptionLabels`, and `deleteSubscription`. These manage pull subscriptions with stable resource names; creation, absolute label replacement, and deletion use safe repeat semantics. The [subscription API reference](https://docs.cloud.google.com/pubsub/docs/reference/rest/v1/projects.subscriptions) defines these endpoints. Push delivery configuration, snapshots, seek, IAM, and schema administration remain outside this connector's current scope.

In [Google Cloud Console](https://console.cloud.google.com/apis/credentials), select a project, enable Pub/Sub API, configure OAuth consent with the `pubsub` scope, and create a Web application client with the exact redirect URI displayed by Dex Web. Supply the project ID from Pub/Sub > Topics as operation input. The [topics.list API reference](https://docs.cloud.google.com/pubsub/docs/reference/rest/v1/projects.topics/list) defines the endpoint and pagination.

Live Google consent, token refresh, Pub/Sub reads, and Dex Web field rendering have not been verified in this checkout.
