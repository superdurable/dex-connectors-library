# fofa Python connector

The `dex-connector-fofa` package implements the read-only `SearchDomainAssets` Query. Search public indexed hosts for one domain. Its [runnable Flow](examples/domain_assets/flow.py) uses the static `fofa` connection and persists the public result. The [real Dex integration test](examples/domain_assets/integration_test.py) verifies retry and persistence. The [conformance test](tests/test_conformance.py) replays Go-recorded provider requests and outcomes.

## Run with Dex Web

Create a `fofa` connection using the authorization guide in [connector.yaml](../connector.yaml). With `DEX_PROJECT_*` set, run:

```bash
uv run python -m examples.domain_assets.worker
```

Start `FOFADomainAssetsFlow` with a unique Flow ID and input `domain: example.com`. The example needs an authorized provider account for live requests.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/fofa-python-fdg --python .venv/bin/python examples/domain_assets/flow.py
```

Live provider authorization and responses remain unverified.
