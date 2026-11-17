# Box new file upload example

Run `go run ./examples/file-create` from `connectors/box/go` with a Dex project
configured for the static `box` connection. Generate the FDG 2.0 view with
`dexcli visualize go/examples/file-create/flow/workflow.go --schema-version 2.0 --lint app`
from the connector directory.

The Flow accepts the destination `parentId` (`0` for root), a unique file
`name`, and complete `contentBase64` bounded by the connector's upload limit.
Find the destination ID in Box Web's `/folders/<ID>` URL. Box creates a new
file; the result contains its ID, name, SHA-1, and parent ID, never its bytes.
The checked-in integration test uses a provider mock and a real Dex Worker.
