# Weiling Python connector

`dex-connector-weiling` reads the accessible Weiling department directory through a Dex Query.

The [runnable Flow](examples/list_departments/README.md) starts a Worker and Client against Dex, uses a static `ConnectionName`, and verifies the `listed` branch with a synthetic provider. Configure credentials through Dex Connections before using the real provider.

The package replays the [Go-recorded scenarios](../../../conformance/connectors/weiling/scenarios.json) for provider request and decision parity. Real Weiling authorization has not been verified.
