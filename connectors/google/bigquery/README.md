# Google BigQuery connector

The Go, Python, and TypeScript `listDatasets` Query lists one bounded page of datasets visible to the connected Google account in a specified project. The runnable [Go](go/examples/datasets/README.md), [Python](python/examples/datasets/README.md), and [TypeScript](typescript/examples/datasets/README.md) Flows use the static `google-bigquery` Dex Web connection. It returns dataset IDs, project IDs, display names, and the next page token. Run queries and row insertion remain in the backlog.

In [Google Cloud Console](https://console.cloud.google.com/apis/credentials), select a project, enable BigQuery API, configure OAuth consent with the `bigquery.readonly` scope, and create a Web application client with the exact redirect URI displayed by Dex Web. Supply the project ID from BigQuery Studio > Explorer as operation input; leave the connection defaults blank unless a trusted gateway or larger response requires an override. The [datasets.list API reference](https://docs.cloud.google.com/bigquery/docs/reference/rest/v2/datasets/list) defines the endpoint and pagination.

Live Google consent, token refresh, BigQuery reads, and Dex Web field rendering have not been verified in this checkout.
