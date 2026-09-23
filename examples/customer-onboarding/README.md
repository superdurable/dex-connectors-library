# Customer onboarding connector example

This Flow performs a profile Query and a credit-grant Mutation in separate
concrete Dex Steps. Query Failure and all three Mutation outcomes have explicit
branches. A Retry is the only connector path returned as a Go error to Dex.

The public Flow input contains only customer business data. A trusted logical
`ConnectionRef` is fixed when the Flow is registered and injected into every
provider Step; a StartFlow caller cannot select another account's connection.
SuperVerse can resolve that logical slot to the authenticated account's
credential without putting credentials or connection identifiers in input.

`RunMutation` derives stable Call ID and provider idempotency key from Flow and
Step execution identity. `GrantCustomerCreditsStep` persists
`CreditGrantReceipt` in the same Dex commit as completion or transition.
Failed enters `CreditGrantFailedStep`; Unknown enters
`ReconcileCreditGrantStep`, which queries provider status without repeating the
Mutation.

The integration fixture also contains a minimal OpenAI streaming Flow. It
registers application-owned structured and text Streams, verifies buffered text
order and final flush, and keeps the completed Step result authoritative.

Run the suite against Dex Server 0.11.1:

```bash
dexcli dev
go test -tags=integration . -count=1 -v
```

Set `DEX_FLOW_SERVICE_ADDRESS` when Dex is not on `127.0.0.1:8801`.
