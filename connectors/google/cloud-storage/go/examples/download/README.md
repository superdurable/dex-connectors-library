# Object download Flow

This runnable Dex Flow downloads one bounded object through the static `google-cloud-storage` Dex Web connection. Configure OAuth as described in the [connector guide](../../../README.md). Start it with a bucket name, an exact object name from `listObjects`, and an optional generation string. It stores the Base64 content and media type in its declared Attribute and completes with the same value. Download only content the application intends to persist in Dex.

From the Go module, generate and lint FDG 2.0:

```sh
dexcli visualize examples/download/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/google-cloud-storage-download-fdg --open=false
```

Run `GOWORK=off go test ./...` from `go/`. With `dexcli dev` running, run `GOWORK=off go test -tags=integration ./examples/download/flow -run TestDownloadUsesDefaultMockWithRealDex` for the real Worker and Client path. With the Google OAuth connection saved in Dex Web, run `GOWORK=off go run ./examples/download` to start the Worker. Live Google authorization and API behavior have not been verified in this checkout.
