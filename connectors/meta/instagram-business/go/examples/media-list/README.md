# Instagram media listing Flow

This runnable Worker registers `InstagramBusinessMediaList` for `listMedia` and `InstagramBusinessAccountLookup` for `getAccount` through the static `instagram-business` connection. Each Flow stores its typed result in a registered Attribute. Optional branches are unwired and fail the Flow when selected.

From the repository root, check the FDG 2.0 graph:

```sh
dexcli visualize connectors/meta/instagram-business/go/examples/media-list/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/instagram-media-fdg --open=false
dexcli visualize connectors/meta/instagram-business/go/examples/media-list/flow/account_lookup.go --schema-version 2.0 --lint app --json --out /tmp/instagram-account-fdg --open=false
```

Configure `instagram-business / instagram-business` in Dex Web using the [connector guide](../../../README.md). From the Go module, run `GOWORK=off go run ./examples/media-list`. Start `InstagramBusinessMediaList` with a unique Flow ID and `{"igUserId":"17900000000000001"}`. Pass `nextCursor` back as `after` for another page. Start `InstagramBusinessAccountLookup` with `{"igUserId":"17800000000000001"}` to read the account ID, username, and media count.

For a mock backed real Dex check, start `dexcli dev -open=false -dex-port 9480 -web-port 9481` and run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:9480 GOWORK=off go test -tags=integration ./examples/media-list/flow -run 'Test(AccountLookup|MediaList)UsesDefaultMockWithRealDex'`. Live Meta authorization remains unverified.
