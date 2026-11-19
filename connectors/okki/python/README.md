# Okki Python connector

`dex-connector-okki` exposes the `listCustomerFields` Query Step through `dex_connectors.okki`. Configure the static `okki` connection using the [connector guide](../README.md).

The [runnable listCustomerFields Flow](examples/list_customer_fields/) has a Worker entrypoint and real Dex integration test. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/okki-python-fdg --python .venv/bin/python examples/list_customer_fields/flow.py
```
