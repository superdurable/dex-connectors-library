# GitHub Python connector

`dex-connector-github` implements the six GitHub Query operations declared in
[`connector.yaml`](../connector.yaml): `getAuthenticatedProfile`,
`listPublicRepositories`, `listMergedPullRequests`, `listPullRequestFiles`,
`listReleases`, and `listCommits`. Its Python release is `v0.1.0`; the Go release
version remains independent.

The generated project connection resolves credentials at call time and uses the
manifest-driven OAuth refresh driver. It requests `read:user`, `user:email`, and
`offline_access` during authorization. The provider responses must advertise
exactly `read:user` and `user:email` in `X-OAuth-Scopes`; broader scopes select
`insufficientScope`. The package never stores tokens in Flow input, Attributes,
Results, receipts, or logs.

The [profile example](examples/profile/README.md) is the runnable consuming
application. Its Worker entrypoint is `examples/profile/main.py`; the Flow
source is `examples/profile/flow.py`. The real Dex integration test runs that
Flow with an inert local provider stand-in. Provider replay tests use the
Go-recorded scenarios in [`conformance/connectors/github`](../../../conformance/connectors/github/).

From this directory, run `uv sync` and `uv run pytest -q tests`. With a local
Dex stack running, set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address and run
`uv run pytest -q examples/profile/integration_test.py`. The local integration
test does not authorize against GitHub.com. Use Dex Web Connections and the
GitHub OAuth App guide in the manifest for live provider authorization.
