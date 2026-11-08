# Object list Flow

Run the Worker with a `google-cloud-storage` Dex Web connection configured from the [connector setup](../../../README.md). The Flow starts with a `ListObjectsInput` containing a bucket name, optional prefix, page size, and page token. It stores the returned page in its declared Attribute and completes with that page. The checked-in provider mock supplies one `example.txt` object with generation `123` for deterministic local verification.

From this module, generate and inspect the FDG 2.0 graph:

```sh
dexcli visualize examples/objects/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/google-cloud-storage-objects-fdg.json --open=false
```

With a Dex stack and connection available, run `go run ./examples/objects`. The mock-backed integration test is `GOWORK=off go test -tags=integration ./examples/objects/flow -run TestObjectsUsesDefaultMockWithRealDex`. The Google provider has not been authorized or called in this checkout.
