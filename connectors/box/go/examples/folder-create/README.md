# Box folder-create example

This runnable Flow calls `box.NewCreateFolderStep` with connection name `box`.
Start `BoxCreateFolder` with `{"parentId":"0","name":"Reports"}`; `0` is the
root folder. The authorized user needs write permission on the parent folder.

From the connector directory, run `dexcli visualize go/examples/folder-create/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/box-folder-create-fdg`. Start Dex with `dexcli dev -open=false -dex-port 9100 -web-port 9101`, set `DEX_FLOW_SERVICE_ADDRESS` to its printed address, then run `GOWORK=off go test -tags=integration ./examples/folder-create/flow -run '^TestMockBackedFlowWithRealDex$' -count=1 -v` from `go/`.

The mock test exercises Dex Worker, Client, persistence, and the generated
folder Step. Live Box authorization and folder creation remain unverified.
