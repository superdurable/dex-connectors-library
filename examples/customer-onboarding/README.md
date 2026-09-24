# Customer onboarding connector example

This Flow constructs the profile Query, credit-grant Mutation, and
reconciliation Query directly with Connector Dex Step factories. It defines no
provider concrete Step. Generated operation branches each have one typed GoTo
target; Retry is the only Connector path returned as a Go error to Dex.

The public Flow input contains only customer business data. A trusted typed
`httpconnector.Connection` is fixed when the Flow is registered and injected
into every provider Step; a StartFlow caller cannot select another account's
connection. The Connection binds the client and logical `ConnectionRef` while
keeping both out of durable input.

The Mutation factory derives stable Call ID and provider idempotency key from
Flow and Step execution identity. It persists `CreditGrantResult` in the same
Dex commit as its selected transition. `rejected` enters a business failure
Step; `uncertain` enters the reconciliation Query factory without repeating
the Mutation. `StepRef[T]` links factories by stable type while Dex uses the
registered target's Step options.

The integration fixture also creates OpenAI streaming through a Mutation
factory. It directly registers the required Result Attribute and
application-owned structured/text Streams, verifies buffered text order and
final flush, and keeps the committed Result authoritative.

Separate signup fixtures construct the generated GitHub and LinkedIn profile
Query factories. Each registers its required Result Attribute and verifies the
provider Result commits atomically with the terminal transition. The LinkedIn
fixture reads only OIDC UserInfo claims; the App-owned callback validates OIDC
state, nonce, issuer, audience, PKCE, and the ID token before the Flow starts.

Dex CLI 0.11.3 renders these canonical factory nodes and their typed branch,
Result Attribute, and Stream edges in Dex Web 2.0.

Run the suite against Dex Server 0.11.3:

```bash
dexcli dev
go test -tags=integration . -count=1 -v
```

Set `DEX_FLOW_SERVICE_ADDRESS` when Dex is not on `127.0.0.1:8801`.
