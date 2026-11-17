# MongoDB connector

`findDocuments` reads a bounded set of documents from one collection with the
official MongoDB Go driver. It accepts a JSON filter, including MongoDB Extended
JSON values, and returns canonical Extended JSON so BSON types are preserved.
It never changes the database. `found` is the happy path, including an empty
result. `truncated`, `providerRejected`, `invalidResponse`, and `defect` are
optional branches. Temporary network, timeout, and retryable read failures
return Retry to Dex. So do DNS lookup failures, including `mongodb+srv://`
SRV lookups; only a URI the driver rejects selects `defect`.

`aggregateDocuments` runs a bounded, read-only aggregation pipeline. It allows common read stages such as `$match`, `$project`, `$group`, `$sort`, `$lookup`, and `$facet`, while rejecting `$out`, `$merge`, server-side JavaScript, and unsupported stages. It appends a final limit to bound returned documents. The happy branch is `aggregated`; truncation, rejection, invalid response, and local defects remain optional branches. Unknown read outcomes retry safely.

See the [MongoDB aggregation guide](https://www.mongodb.com/docs/drivers/go/v1.x/fundamentals/aggregation/) for pipeline behavior and stage syntax.

n8n also offers document insert, update, replace, and delete operations. The current connection guide provisions a read-only database user, so those writes require a new permission contract and are deferred. Search-index changes are administrative schema work and are outside this connector's read scope.

The connector uses the native MongoDB wire protocol. Atlas Data API has been
retired. The Go module currently pins driver v1.17.10 because this repository's
CI runs Go 1.24 and driver v2.9 requires Go 1.25. Upgrade the driver after the
repository raises its Go baseline.

## Connection

In Dex Web **Connections**, create the static connection `mongodb` used by the
[document discovery example](go/examples/document-discovery/README.md). Set
`database` to the database name and save a complete `mongodb://` or
`mongodb+srv://` connection URI as the secret `connection_uri`. The manifest's
authorization guide explains the Atlas Database Access, Network Access, and
Connect pages. Use a dedicated read-only database user and allow the Worker's
egress address. A self-hosted server also works with a URI supplied by its
administrator. The URI is resolved for each call; it never belongs in Flow
input or Results.

The defaults are a 15-second request timeout, 100 returned documents, and
1 MiB of result JSON. The connector caps overrides at 1000 documents and
16 MiB. It fetches one extra document to detect count truncation. The
collection is supplied in Flow input; it may contain letters, digits,
underscores, hyphens, and dots, and must begin with a letter or underscore.
The filter is at most 64 KiB and disallows server-side JavaScript operators.
The URI and database name are checked before use.

## Example and verification

The checked-in [document discovery Flow](go/examples/document-discovery/README.md)
uses `mongodb.NewFindDocumentsStep` and a static Dex connection. Its default
mock runs through a real Dex Worker and Client. The provider tests use the
official driver's mock deployment to verify the wire command, BSON encoding,
count truncation, rejection, and retry classification. A real MongoDB read,
Atlas authorization, and network access have not yet been verified.

From `go/`, run:

```bash
GOWORK=off go test ./...
GOWORK=off go vet ./...
```

The [Python package](python/README.md) uses PyMongo's native asynchronous
driver. The [TypeScript package](typescript/README.md) uses MongoDB's Node.js
driver. Both include runnable Dex Flows, driver unit tests, and real Dex mock
integration tests. Their native MongoDB service integration tests run in CI;
live Atlas authorization remains unverified.
