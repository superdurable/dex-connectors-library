# GitHub Connector for TypeScript

`@superdurable/connector-github` provides Dex Query operations for an authenticated profile, public repositories, merged pull requests, pull request files, releases, and commits. The generated Step factories and connection bindings use the same connector and branch IDs as the Go module.

The OAuth connection requests `read:user`, `user:email`, and `offline_access`. Dex stores the access and rotating refresh tokens in the named project connection. The package uses manifest-generated credential decoders and refresh declarations. GitHub Enterprise Server may return a non-expiring access token without refresh material.

Use `newProjectConnection(project, ConnectionName)` in a Worker after loading its Dex project. The [repository releases example](examples/repository-releases/README.md) is a runnable Flow with a static connection name and a persisted release page. Its optional failure branches are intentionally unwired, so an authorization or provider rejection fails the Flow.

The package requires Node.js 22 or 24 and a Dex TypeScript SDK meeting the declared peer minimum. `npm run check` runs the package tests, type checker, and linter. `npm run test:integration` runs the example through a real Dex Worker and Client against the server named by `DEX_FLOW_SERVICE_ADDRESS`.

Connection settings include `baseUrl`, `apiVersion`, `timeout` (positive Go duration, converted to milliseconds), `maxResponseBytes`, and bounds for repository counts and returned text. The connector never places credentials or raw provider error bodies in Flow state. See the [connector contract](../README.md) for operation details and authorization guidance.
