# Resend create-topic example

This Flow uses `resend.NewCreateTopicStep` and the static Dex Web connection
name `resend`. The input supplies a unique topic name (at most 50 characters),
an immutable `defaultSubscription` of `opt_in` or `opt_out`, an optional
description (at most 200 characters), and `visibility` of `public` or
`private`. Blank visibility uses Resend's private default. Use a full-access
API key from [Resend API Keys](https://resend.com/api-keys); find created topics
under [Resend Topics](https://resend.com/topics).

From the connector directory, run `dexcli visualize
go/examples/create-topic/flow/workflow.go --schema-version 2.0 --lint app
--json --out /tmp/resend-create-topic-fdg`. Start `dexcli dev -open=false
-dex-port 9100 -web-port 9101`, pass its printed Flow service address as
`DEX_FLOW_SERVICE_ADDRESS`, then run `GOWORK=off go test -tags=integration
./examples/create-topic/flow -run '^TestMockBackedFlowWithRealDex$' -count=1 -v`
from `go/`. Stop Dex afterward. Live provider authorization remains unverified.
