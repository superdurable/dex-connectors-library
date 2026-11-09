# Cloud Firestore connector

The Go, Python, and TypeScript `listDocuments` Queries list a bounded page of document resource names in a top-level collection of the `(default)` database. The runnable [Go](go/examples/documents/README.md), [Python](python/examples/documents/README.md), and [TypeScript](typescript/examples/documents/README.md) Flows use the static `google-firestore` Dex Web connection. Document contents, nested collections, writes, and change events remain outside this initial capability.

In [Google Cloud Console](https://console.cloud.google.com/apis/credentials), select a project, enable Cloud Firestore API, configure OAuth consent with the `datastore` scope, and create a Web application client with the exact redirect URI displayed by Dex Web. Find the project and collection IDs under Firestore > Databases > (default) > Documents. The [documents.list API reference](https://firebase.google.com/docs/firestore/reference/rest/v1/projects.databases.documents/list) defines the endpoint and pagination.

Live Google consent, token refresh, Firestore reads, and Dex Web field rendering have not been verified in this checkout.
