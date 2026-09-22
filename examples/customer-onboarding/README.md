# Customer onboarding connector example

This Flow performs one safe profile Query and one credit-grant Action. The
Action receives a UUID `CallID` created by the previous Step and persisted in
both the next Step input and the `CreditGrantCallID` Attribute. A transport
timeout transitions to a receipt lookup Step instead of blindly repeating the
mutation.

Run the acceptance test against a Dex Server:

```bash
dexcli dev
go test -tags=integration . -count=1 -v
```

Set `DEX_FLOW_SERVICE_ADDRESS` when Dex is not on `127.0.0.1:8801`.
