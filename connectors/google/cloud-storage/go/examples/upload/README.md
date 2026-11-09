# Object upload Flow

This runnable Dex Flow uploads one object through the static `google-cloud-storage` connection. See the [connector guide](../../../README.md) for OAuth setup. Start it with the destination `bucket` and `name`, complete `contentBase64`, and an HTTP `contentType` such as `text/plain`. The result contains the object name and generation. The default provider mock returns `example.txt` at generation `123` for deterministic tests.

From the Go module, generate and lint the FDG 2.0 graph:

```sh
dexcli visualize examples/upload/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/google-cloud-storage-upload-fdg --open=false
```

Run `GOWORK=off go test ./...` from `go/`. With `dexcli dev` running, run `GOWORK=off go test -tags=integration ./examples/upload/flow -run TestUploadUsesDefaultMockWithRealDex` to exercise the real Worker and Client path. With the Google OAuth connection saved in Dex Web, run `GOWORK=off go run ./examples/upload` to start the Worker. Live Google authorization and upload behavior have not been verified in this checkout.
