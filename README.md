# Dex Connectors Library

Dex Connectors Library is the open-source boundary between Dex Flows and
external providers. Credentials stay outside durable Flow state. Provider call
identity comes from Dex Step execution identity, and every provider outcome is
explicit.

[![Dex Connectors](https://superdurable.github.io/dex-connectors-library/card.png)](https://superdurable.github.io/dex-connectors-library/)

The directory lists each published connector, its company, reusable UI units,
triggers, and operations. Search matches all of those names and descriptions.
The first folder under `connectors/` is the company name, and that folder
contains `logo.svg`.

Each connector is its own Go module. Release tags use the module directory,
such as `connectors/github/vX.Y.Z` and `connectors/google/gmail/vX.Y.Z`.
The manifest `metadata.version` is the release version. The root
`connectors.yaml` registry is the source for the public catalog.

- [Connector contract](docs/connector-contract.md)
- [Architecture](docs/architecture.md)
- [Manifest authoring](docs/manifest-authoring.md)
- [Versioning and releases](docs/versioning-and-releases.md)
- [Acceptance](docs/acceptance.md)

Maintainers run `make check`. The directory site is in `site/`. Local preview:

```bash
cd site
npm ci
npm run dev
```

The preview image at `card.png` is generated when the catalog is published.
It is not redrawn when someone opens this README.
