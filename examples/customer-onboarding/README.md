# Customer onboarding connector example

This Flow performs a profile Query and a credit-grant Mutation in separate
concrete Dex Steps. `RunMutation` derives its stable CallID from the Flow and
Step execution identities. The application never creates or persists a call
key before invoking the connector.

`GrantCustomerCreditsStep` persists `CreditGrantReceipt` in the same Dex
commit that completes or transitions. `FAILED` enters an explicit failure
Step. `UNKNOWN` enters `ReconcileCreditGrantStep`, which reads the committed
receipt and queries provider status without repeating the Mutation.

Run the acceptance test against Dex Server 0.11.1:

```bash
dexcli dev
go test -tags=integration . -count=1 -v
```

Set `DEX_FLOW_SERVICE_ADDRESS` when Dex is not on `127.0.0.1:8801`.
