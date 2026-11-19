# feihan Python connector

The `dex-connector-feihan` package implements the read-only `QueryRechargeOrder` Query. Query a known Feihan recharge order's status. Its [runnable Flow](examples/recharge_order/flow.py) uses the static `feihan` connection and persists the public result. The [real Dex integration test](examples/recharge_order/integration_test.py) verifies retry and persistence. The [conformance test](tests/test_conformance.py) replays Go-recorded provider requests and outcomes.

## Run with Dex Web

Create a `feihan` connection using the authorization guide in [connector.yaml](../connector.yaml). With `DEX_PROJECT_*` set, run:

```bash
uv run python -m examples.recharge_order.worker
```

Start `FeihanRechargeOrderFlow` with a unique Flow ID and input `merchant_order_id: ORDER123`. The example needs an authorized provider account for live requests.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/feihan-python-fdg --python .venv/bin/python examples/recharge_order/flow.py
```

Live provider authorization and responses remain unverified.
