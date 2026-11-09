# Constant Contact Python contact inspection

This runnable Flow calls `getContact` through the static `constantcontact` connection, stores the result in an Attribute, and completes. Configure a Constant Contact V3 OAuth application in Dex Web Connections with `contact_data` and `offline_access`, then run from `connectors/constantcontact/python`:

```bash
uv run python -m examples.contact_inspection.worker
```

Start Flow type `ConstantContactContactInspectionPython` with a unique Flow ID and input `{"contactId":"1898ae62-4752-11e9-9c8a-fa163e6b01c1"}`. Replace the sample UUID with a real contact ID. Optional outcome branches are unwired and fail the Flow when selected.

For strict FDG 2.0 generation, run from the repository root after `uv sync --locked --project connectors/constantcontact/python`:

```bash
dexcli visualize ./connectors/constantcontact/python/examples/contact_inspection/flow.py --python ./connectors/constantcontact/python/.venv/bin/python --schema-version 2.0 --json --out /tmp/constantcontact-python-fdg
```

The generated FDG must report `valid: true` with no diagnostics.
