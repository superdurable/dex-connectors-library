# Google Cloud Pub/Sub connector

The Go, Python, and TypeScript `listTopics` Queries list one bounded page of topic resource names visible to the connected Google account in a specified project. The runnable [Go](go/examples/topics/README.md), [Python](python/examples/topics/README.md), and [TypeScript](typescript/examples/topics/README.md) Flows use the static `google-pubsub` Dex Web connection. Publishing messages and subscription delivery remain in the backlog.

In [Google Cloud Console](https://console.cloud.google.com/apis/credentials), select a project, enable Pub/Sub API, configure OAuth consent with the `pubsub` scope, and create a Web application client with the exact redirect URI displayed by Dex Web. Supply the project ID from Pub/Sub > Topics as operation input. The [topics.list API reference](https://docs.cloud.google.com/pubsub/docs/reference/rest/v1/projects.topics/list) defines the endpoint and pagination.

Live Google consent, token refresh, Pub/Sub reads, and Dex Web field rendering have not been verified in this checkout.
