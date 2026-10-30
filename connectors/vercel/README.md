# Vercel connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/vercel/go@v0.2.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/vercel`.

The `vercel` Go module exposes typed Dex Steps for Vercel. Its [runnable lookup Flow](go/examples/vercel-lookup/README.md) uses the static connection name `vercel`.

## Operations

| Factory | Endpoint | Input | Output | Required branch | Optional branches |
| --- | --- | --- | --- | --- | --- |
| NewListDeploymentsStep | GET /v6/deployments | optional `TeamID` | `DeploymentsPage.Deployments` and `Pagination.Next` | `listed` | `providerRejected`, `invalidResponse`, `defect |
| NewGetDeploymentStep | GET /v13/deployments/{idOrUrl} | deployment ID or hostname in `IDOrURL`, optional `TeamID` | `Deployment` with identity, URL, and state fields | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect |

Both operations are read-only Queries. The list operation returns one provider page.

## Authorization

Create a secret access token in [Account Settings > Tokens](https://vercel.com/account/tokens) for the personal account or intended team. The connector sends `Authorization: Bearer <token>`. Set `TeamID` on an operation to read team deployments; blank uses the personal account.

`baseUrl`, `timeout`, and `maxResponseBytes` have manifest defaults. The connection form gives the provider URL, setup path, units, and blank behavior for every field. Secrets are resolved for each call and are never copied into Flow input, Results, Failures, receipts, or logs.

## Failure behavior

| Response | Outcome |
| --- | --- |
| HTTP 200 with a valid provider result | Required success branch |
| HTTP 404 for a named resource | `notFound` when declared, otherwise `providerRejected` |
| Other conclusive 4xx or redirects | `providerRejected` |
| HTTP 408, 409, 429, 5xx, or transport failure | Retry under the default one-hour policy |
| Malformed or oversized response | `invalidResponse` |
| Invalid local input or credentials | `defect` before sending |

HTTP redirects are not followed. Provider error bodies are never copied into failures. Unwired optional branches fail the Flow.

## Mocks

The generated `vercelmock` package installs these default answers:

- `listDeployments`: one ready `meridian-web` deployment and no next cursor.
- `getDeployment`: the same `dpl_meridian1` deployment.

The [mock-backed example test](go/examples/vercel-lookup/flow/workflow_mock_integration_test.go) runs the first default through a real Dex Worker and Client in CI.

## Official API references

- [https://vercel.com/docs/rest-api](https://vercel.com/docs/rest-api)
- [https://vercel.com/docs/deployments/managing-deployments](https://vercel.com/docs/deployments/managing-deployments)
- [https://vercel.com/kb/guide/how-do-i-use-a-vercel-api-access-token](https://vercel.com/kb/guide/how-do-i-use-a-vercel-api-access-token)
- [Vercel SDK deployment response fields](https://github.com/vercel/sdk/blob/main/docs/models/deployments.md)

## Verification limits

The provider calls have not been exercised with live account credentials. Real Dex execution, release compatibility, and provider authorization remain unverified locally. Unlisted response fields and undocumented status details are not asserted.
