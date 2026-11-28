# Gusto employee lookup Flow (Python)

This runnable Flow calls `getEmployee` through the static `gusto` Dex Web connection. Configure OAuth in the [connector guide](../../../README.md), then run `uv run python -m examples.employee.worker` from the Python package directory. The result is stored in a declared Attribute.

Run `dexcli visualize python/examples/employee/flow.py --schema-version 2.0 --lint app --json --python python/.venv/bin/python --out /tmp/gusto-python-fdg --open=false` from the connector directory. With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `uv run pytest examples/employee/integration_test.py` from the Python package directory to verify the real Worker and Client path against a local provider fixture. Live Gusto authorization has not been verified.

The Worker also serves [the employee page Flow](list_flow.py). Start `GustoEmployeeListPython` with the authorized company UUID as `company_id`, a one-based `page`, and a `per_page` value from 1 to 100. The Flow returns employee identity fields in provider order, including an empty employee list for an empty page.

From the Python package directory, validate the page Flow with `dexcli visualize examples/employee/list_flow.py --schema-version 2.0 --lint app --json --open=false --python .venv/bin/python --out /tmp/gusto-python-page`.
