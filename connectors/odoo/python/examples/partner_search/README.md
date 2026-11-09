# Odoo Python partner search

This runnable Flow calls `searchPartners` through the static `odoo` connection, persists the partner page, and completes. Optional branches are unwired and fail the Flow when selected. Configure `baseUrl`, `database`, and secret `apiKey` using the [connection guide](../../../README.md).

From `connectors/odoo/python`, run `uv run python -m examples.partner_search.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `OdooPartnerSearchPython` with a unique Flow ID and input `{"namePattern":"%Acme%","limit":5}`.

Generate and lint the Flow Definition Graph 2.0 from the same directory:

```bash
dexcli visualize examples/partner_search/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/odoo-python-fdg --open=false --python .venv/bin/python
```
