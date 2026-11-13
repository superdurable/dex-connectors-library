# EMQX Cloud deployment list Flow

Run the Worker from this module after starting `dexcli dev`:

```sh
GOWORK=off go run ./examples/deployment-list
```

Create a Dex Web connection named `emq` using the [connection guide](../../../README.md#connection), then start the `EMQDeploymentList` Flow with `{}` as input. The Flow completes with the deployments visible to that key. Test the default mock against a real Dex Server with:

```sh
dexcli dev -dex-port 9021 -web-port 9022 -open=false
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:9021 GOWORK=off go test -tags integration ./examples/deployment-list/flow -run TestDeploymentListUsesDefaultMockWithRealDex
```

Validate its Flow Definition Graph 2.0 with:

```sh
dexcli visualize ./examples/deployment-list/flow/workflow.go --schema-version 2.0 --json --out /tmp/emq-deployment-list
```
