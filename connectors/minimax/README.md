# MiniMax connector

The Go connector reads MiniMax China's [Token Plan usage
API](https://platform.minimaxi.com/subscribe/token-plan) through
`GET /v1/token_plan/remains`. It exposes the `GetTokenPlanRemains` Query Step
and a runnable [`token-plan-remains` Flow](go/examples/token-plan-remains/README.md).

Install `github.com/superdurable/dex-connectors-library/connectors/minimax/go@v0.1.0`
after the first release. The connection requires a **Token Plan subscription
API Key** from the [MiniMax Token Plan page](https://platform.minimaxi.com/subscribe/token-plan),
typically beginning `sk-cp-`. A standard pay-as-you-go key belongs to a
different product and may be rejected by this endpoint. The secret stays in
Dex Connections and is sent only as a Bearer header. Provider error bodies
and transport URLs are never copied into Dex results or logs.

The `fetched` branch returns provider-reported per-group remaining percentages,
reset times in Unix milliseconds, and raw status codes. It does not derive
remaining calls from ambiguous provider count fields. An empty group list is
a successful empty result; a missing or expired subscription can instead
produce a provider error status and select `providerRejected`. HTTP 408, 429,
5xx, transport failures, and provider rate-limit codes 1002/1039 retry under
Dex's default one-hour budget. Authentication code 1004 and quota code 1008
select `providerRejected`; malformed or oversized replies select
`invalidResponse`. Missing credentials select `defect`.

## Verification

```bash
cd connectors/minimax/go
GOWORK=off go test ./...
GOWORK=off go vet ./...
```

Provider tests use a local HTTP server. The example's integration test uses a
real Dex Worker and Client with a generated mock. A real MiniMax Token Plan
key and Dex Web connection are needed for live verification.

References: [MiniMax Token Plan usage endpoint](https://platform.minimaxi.com/subscribe/token-plan)
and [MiniMax's CLI quota response fixture](https://github.com/MiniMax-AI/cli/blob/main/test/fixtures/quota-response.json).
