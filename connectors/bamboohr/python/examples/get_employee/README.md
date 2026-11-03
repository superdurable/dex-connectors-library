# BambooHR employee lookup example

Start `dexcli dev`, create a BambooHR connection named `bamboohr` in Dex Web, and supply `companyDomain` and an API key through that connection. Run `uv run python -m examples.get_employee.worker` in one terminal, then `uv run python -m examples.get_employee.start <unique-flow-id> examples/get_employee/sample-input.json` in another. The Flow returns the selected fields and reports omitted fields; `notFound` and other optional branches fail the Flow unless the application wires a response.

For a local provider stand-in, the integration test uses the same Flow against a real Dex Worker and Client. It does not use a live BambooHR account.

With Dex CLI v1.28.0 or newer, generate the strict Flow Definition Graph 2.0
from the checked-in Flow source:

```sh
dexcli visualize examples/get_employee/flow.py --schema-version 2.0 --json --out /tmp/bamboohr-get-employee
```

Run this command from `connectors/bamboohr/python/`. The source's connector
Step annotations and the package's generated `dex-connector.json` supply the
operation and branch metadata.
