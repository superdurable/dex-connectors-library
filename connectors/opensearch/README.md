# OpenSearch connector

The first Go release provides `searchDocuments`, a bounded Lucene query-string search of one concrete index. It returns total-hit information and each hit's ID, score, and source JSON. Search source is untrusted provider data; restrict index permissions and avoid placing credentials in documents that a Flow can read.

Set `clusterUrl` to the HTTPS REST endpoint from your OpenSearch deployment or managed-service console. In OpenSearch Dashboards, create an internal user under **Security > Internal users** and a role under **Security > Roles** with `indices:data/read/search` for the intended index pattern. Map the user to the role, then save its username and secret password in Dex Connections. See [OpenSearch security permissions](https://docs.opensearch.org/latest/security/access-control/permissions/).

`searchDocuments` selects `found`, `indexNotFound`, `providerRejected`, `invalidResponse`, or `defect`. HTTP 429, 503, other server errors, transport failures, timed-out searches, and shard failures retry under the default one-hour policy. The connector requests `allow_partial_search_results=false`. The [document-search example](go/examples/document-search/README.md) shows the operation-specific Step factory and static `opensearch` connection. Index and delete operations from the backlog remain future additions.
