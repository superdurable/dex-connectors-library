# Gitee connector

The Go connector reads one repository through Gitee's [REST API](https://gitee.com/api/v5/swagger). It exposes the `GetRepository` Query Step and a runnable [`repository-lookup` Flow](go/examples/repository-lookup/README.md). Provide the owner and repository paths from `https://gitee.com/<owner>/<repo>` in the Flow input.

Create a personal access token in [Gitee Personal Access Tokens](https://gitee.com/profile/personal_access_tokens) with the minimum repository-read access needed. Dex stores it as a secret connection credential. Gitee's REST API accepts it as `access_token` in the HTTPS query. The connector disables redirects and does not put URLs, credentials, or provider error bodies into failures or results.

The Query retries HTTP 408, 429, 5xx, and transport failures under Dex's default retry budget. HTTP 404 selects `notFound`; other non-200 statuses select `providerRejected`. Malformed or oversized responses select `invalidResponse`; invalid input or credentials select `defect`.

Install `github.com/superdurable/dex-connectors-library/connectors/gitee/go@v0.1.0` after release. Provider tests use an HTTP test server. The mock-backed example integration test uses a real Dex Worker and Client. Live personal-token authentication and private-repository lookup require a user-configured token and remain unverified by local tests.

The first TypeScript package implements the same `getRepository` Query. Its [runnable example](typescript/examples/repository-lookup/README.md) reads repository metadata through a static Dex connection and includes Go scenario replay and a real Dex integration test with a local provider fixture.
