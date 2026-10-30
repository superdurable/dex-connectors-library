# Netlify connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/netlify/go@v0.2.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/netlify`.

The `netlify` Go module exposes typed Dex Steps for Netlify. Its [runnable lookup Flow](go/examples/netlify-lookup/README.md) uses the static connection name `netlify`.

## Operations

| Factory | Endpoint | Input | Output | Required branch | Optional branches |
| --- | --- | --- | --- | --- | --- |
| NewListSitesStep | GET /api/v1/sites | empty `ListSitesInput` | `SitesPage.Sites` | `listed` | `providerRejected`, `invalidResponse`, `defect |
| NewListDeploysStep | GET /api/v1/sites/{site_id}/deploys | `SiteID`, shown as Project ID in Netlify | `DeploysPage.Deploys` | `listed` | `notFound`, `providerRejected`, `invalidResponse`, `defect |

Both operations are read-only Queries. They return the first provider page; Netlify defaults to at most 100 results per page.

## Authorization

Create a personal access token at [User settings > Applications > Personal access tokens](https://app.netlify.com/user/applications#personal-access-tokens). Grant team access during creation when SSO requires it. The connector sends `Authorization: Bearer <token>`.

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

The generated `netlifymock` package installs these default answers:

- `listSites`: the Meridian Web site.
- `listDeploys`: one ready deploy for that site.

The [mock-backed example test](go/examples/netlify-lookup/flow/workflow_mock_integration_test.go) runs the first default through a real Dex Worker and Client in CI.

## Official API references

- [https://docs.netlify.com/api-and-cli-guides/api-guides/get-started-with-api/](https://docs.netlify.com/api-and-cli-guides/api-guides/get-started-with-api/)
- [https://docs.netlify.com/manage/accounts-and-billing/user-settings/](https://docs.netlify.com/manage/accounts-and-billing/user-settings/)

## Verification limits

The provider calls have not been exercised with live account credentials. Real Dex execution, release compatibility, and provider authorization remain unverified locally. Unlisted response fields and undocumented status details are not asserted.
