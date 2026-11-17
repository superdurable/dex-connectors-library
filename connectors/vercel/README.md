# Vercel connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/vercel/go@v0.3.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/vercel`.

The `vercel` Go module exposes typed Dex Steps for Vercel. Its [runnable lookup Flow](go/examples/vercel-lookup/README.md) uses the static connection name `vercel`.

The Python package is [`dex-connector-vercel`](python/README.md), imported as
`dex_connectors.vercel`.

The TypeScript package is [`@superdurable/connector-vercel`](typescript/README.md), with a checked-in Dex lookup Flow.

## Operations

| Factory | Endpoint | Input | Output | Required branch | Optional branches |
| --- | --- | --- | --- | --- | --- |
| NewListDeploymentsStep | GET /v6/deployments | optional `TeamID` | `DeploymentsPage.Deployments` and `Pagination.Next` | `listed` | `providerRejected`, `invalidResponse`, `defect |
| NewGetDeploymentStep | GET /v13/deployments/{idOrUrl} | deployment ID or hostname in `IDOrURL`, optional `TeamID` | `Deployment` with identity, URL, and state fields | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect |
| NewCancelDeploymentStep | PATCH /v12/deployments/{id}/cancel | deployment ID and optional team ID | canceled `Deployment` | `canceled` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| NewDeleteDeploymentStep | DELETE /v13/deployments/{id} | deployment ID and optional team ID | `DeletedDeployment` | `deleted` | `providerRejected`, `defect` |
| NewListProjectsStep | GET /v9/projects | optional team ID | `ProjectsPage` | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| NewGetProjectStep | GET /v9/projects/{idOrName} | project ID or name, optional team ID | `Project` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| NewCreateProjectStep | POST /v9/projects | project name, optional team ID | `Project` | `created` | `providerRejected`, `uncertain`, `defect` |
| NewUpdateProjectStep | PATCH /v9/projects/{idOrName} | project ID, replacement name, optional team ID | `Project` | `updated` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| NewDeleteProjectStep | DELETE /v9/projects/{idOrName} | project ID or name, optional team ID | `DeletedProject` | `deleted` | `providerRejected`, `defect` |
| NewListProjectDomainsStep | GET /v9/projects/{idOrName}/domains | project ID, optional team ID | `ProjectDomainsPage` | `listed` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| NewGetProjectDomainStep | GET /v9/projects/{idOrName}/domains/{domain} | project ID, domain, optional team ID | `ProjectDomain` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| NewAddProjectDomainStep | POST /v10/projects/{idOrName}/domains | project ID, domain, optional team ID | `ProjectDomain` | `added` | `providerRejected`, `uncertain`, `defect` |
| NewUpdateProjectDomainStep | PATCH /v9/projects/{idOrName}/domains/{domain} | project ID, domain, Git branch, optional team ID | `ProjectDomain` | `updated` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| NewRemoveProjectDomainStep | DELETE /v9/projects/{idOrName}/domains/{domain} | project ID, domain, optional team ID | `RemovedProjectDomain` | `removed` | `providerRejected`, `defect` |
| NewVerifyProjectDomainStep | POST /v9/projects/{idOrName}/domains/{domain}/verify | project ID, domain, optional team ID | `ProjectDomain` | `verified` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

Deployment, project, and domain reads are safe Queries. Cancellation, deletion, absolute project rename, domain branch assignment, domain removal, and domain verification are safe Mutations. An already absent deployment, project, or domain assignment counts as deleted or removed. Project creation and domain assignment are limited to three possible sends per call. Vercel provides no request marker or idempotency key for these operations, so a lookup by project or domain name cannot establish which call created it. After an unknown result, the connector resends within the cap and selects `uncertain` when the cap is reached or a later conclusive response cannot establish the earlier outcome. Creating deployments requires file upload and is deferred as a binary-heavy operation. Environment variable operations are omitted because variable values may be secrets and the connector contract forbids placing secrets in Flow input or output.

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
- [Vercel project domain API](https://vercel.com/docs/rest-api/projects/add-a-domain-to-a-project)
- [https://vercel.com/docs/deployments/managing-deployments](https://vercel.com/docs/deployments/managing-deployments)
- [https://vercel.com/kb/guide/how-do-i-use-a-vercel-api-access-token](https://vercel.com/kb/guide/how-do-i-use-a-vercel-api-access-token)
- [Vercel SDK deployment response fields](https://github.com/vercel/sdk/blob/main/docs/models/deployments.md)

## Verification limits

The provider calls have not been exercised with live account credentials. Real Dex execution, release compatibility, and provider authorization remain unverified locally. Unlisted response fields and undocumented status details are not asserted.
