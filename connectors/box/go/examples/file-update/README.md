# Box file-update example

This runnable Flow updates one file with `box.NewUpdateFileStep` and completes
with its metadata and shared link. It uses the static Dex Web connection name
`box`. Find a decimal file ID after `/files/` in the Box Web URL. Supply a
new file name, a destination parent folder ID (`0` is root), or
`sharedLinkAccess` as `open`, `company`, or `collaborators`; blank fields are
left unchanged. The authorized user needs write permission for the file.

From the connector directory, run `dexcli visualize go/examples/file-update/flow/workflow.go
--schema-version 2.0 --lint app --json --out /tmp/box-file-update-fdg` to
validate the Flow Definition Graph 2.0. Start Dex with `dexcli dev -open=false
-dex-port 9100 -web-port 9101`, set `DEX_FLOW_SERVICE_ADDRESS` to its printed
Flow service address, then run `GOWORK=off go test -tags=integration
./examples/file-update/flow -run '^TestMockBackedFlowWithRealDex$' -count=1 -v`
from `go/` for the deterministic mock path. Stop the Dex process afterward.

To use Box itself, authorize the OAuth app in Dex Web and start
`BoxUpdateFile` with input such as:

```json
{"fileId":"12345","name":"renamed.txt","parentId":"0","sharedLinkAccess":"open"}
```

Live Box authorization, rename, move, and shared link behavior have not been
verified without provider credentials.
