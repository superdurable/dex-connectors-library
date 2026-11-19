# Box file sharing example

Run `go run ./examples/file-share` from `connectors/box/go` with a Dex project
configured for the static `box` connection. Use `dexcli visualize
go/examples/file-share/flow/workflow.go --schema-version 2.0 --lint app` from
the connector directory to generate its FDG 2.0 view.

The Flow accepts a decimal Box `fileId` and an `access` value of `open`,
`company`, or `collaborators`. Find the ID in the Box Web `/files/<ID>` URL.
`shareFile` sets that audience and returns the shared link URL when Box permits
it. The checked-in integration test starts a real Dex Worker against a mock
provider; live Box OAuth and sharing policy still need provider verification.
