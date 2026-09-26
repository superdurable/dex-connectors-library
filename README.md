# Dex Connectors Library

Open-source connectors that take a Dex process from a prototype to production.
Each connector keeps credentials outside durable Flow state and makes every
provider outcome explicit.

[![Dex Connectors](https://superdurable.github.io/dex-connectors-library/card.png)](https://superdurable.github.io/dex-connectors-library/)

The first directory under `connectors/` is the company. Its name must match
`metadata.company`, and the directory must contain `logo.svg`.

The Go SDK and every connector are independent modules. Tags use the module
directory, such as `sdkgo/vX.Y.Z` and `connectors/slack/vX.Y.Z`.

Read [the Connector contract](docs/connector-contract.md),
[architecture](docs/architecture.md),
[manifest authoring](docs/manifest-authoring.md), and
[versioning and releases](docs/versioning-and-releases.md).

Maintainers run `make check`. Preview the directory locally with:

```bash
cd site
npm ci
npm run dev
```
