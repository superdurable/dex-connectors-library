# Shoplazza shop inspection Flow

This runnable Worker starts `ShoplazzaShopInspection` with `{}` and reads the configured store's ID, name, public domain, and currency. The `found` branch stores that result in a Dex Attribute and completes. Optional error branches are unwired, so a definitive rejection fails the Flow. The static connection name is `shoplazza`.

1. Start `dexcli dev -dex-port 9721 -web-port 9722 -open=false`.
2. In Dex Web Connections, configure the Shoplazza private-app connection. The [provider guide](https://www.shoplazza.dev/docs/app/getting-started/create-private-app) starts at the China admin, Apps > Manage Private Apps. Grant only `read_shop`; copy the generated secret Access Token. Enter the store's `*.myshoplaza.com` system domain. Leave timeout and response-byte limits blank for their defaults.
3. Run `GOWORK=off go run ./examples/shop-inspection` from `connectors/shoplazza/go` with `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:9721`.
4. In Dex Web Start Flow, choose `ShoplazzaShopInspection`, enter a unique Flow ID, and use `{}` as the input.

For a mock-only integration check, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:9721 GOWORK=off go test -tags integration ./examples/shop-inspection/flow -run TestShopInspectionUsesDefaultMockWithRealDex -count=1` while the dev server runs.

Generate and validate Flow Definition Graph 2.0 from the checked-in source:

```bash
dexcli visualize ./examples/shop-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/shoplazza-fdg
```
