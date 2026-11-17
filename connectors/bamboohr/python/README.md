# BambooHR Python connector

`dex-connector-bamboohr` provides typed Dex Steps for `getEmployee`,
`findEmployeeByEmail`, `listEmployeeChanges`, `listTimeOffRequests`,
`updateEmployee`, and `addEmployee`. Each uses the branches and defaults in
`connector.yaml`. `addEmployee` uses sync Execute and a persisted dispatch
fence that resends an unknown outcome only while possible sends stay below the
Step's send cap, and selects `uncertain` at the cap. All other optional branches
fail an example Flow unless the application wires them for a business reaction.

Version `0.2.1` resends an `addEmployee` outcome that is unknown, such as a 5xx,
409, or lost response, below the send cap; returns Retry for an unreadable
dispatch checkpoint; and retries an `updateEmployee` write that BambooHR did not
definitely refuse.

Version `0.2.0` adds the `employeeChanged` poll Trigger on `dex-connectors-sdk`
0.12.0's poll Trigger kit. `new_project_employee_changed_trigger_runner` loads
each saved binding's change type (an unsaved binding delivers every change) and
records each change in the binding's project Trigger inbox before its cursor
commits; `new_employee_changed_trigger_runner` takes routes with targets and
cursor stores; `new_employee_changed_trigger` keeps the cursor in memory, as
Go's generated factory does. Each poll makes one `GET /employees/changed`
request, delivers changes at least 30 seconds old by BambooHR's Date header,
oldest first by `lastChanged` and numeric employee ID, at most 500 per page,
and names each event `employee-<id>-<action>-<lastChanged Unix seconds>`. A
429 or 503 pauses every binding of the connection; a 400 rejecting the cursor
starts the binding again at the current time. The cursor document is the Go
connector's byte for byte, so Go and Python replicas share it. `Client` rejects
a `pollInterval` under 10 seconds. The
[employee-changed example](examples/employee_changed/README.md) starts one Flow
per change and reads inserted and updated employees back.

Use the [runnable employee lookup Flow](examples/get_employee/README.md) as the
application entrypoint. Its `worker.py` loads the static `bamboohr` connection
from Dex Web project configuration; `start.py` decodes a bounded JSON request
and starts a unique Flow ID. The [connector README](../README.md#bamboohr-setup)
explains the BambooHR company domain, API key, and provider field vocabulary.
Time off request and daily amounts retain Go-compatible JSON number shapes:
whole amounts serialize as integers, while fractional amounts remain decimals.
Provider IDs outside Go's signed 64-bit range select `invalidResponse`.

Run the package checks with `uv sync --locked`, `uv run --locked pytest tests`,
`uv run --locked ruff check src examples tests`, and
`uv run --locked mypy src examples/get_employee examples/employee_changed tests`.
The conformance test replays the checked-in Go provider recording, and
`tests/test_employee_changed_go_parity.py` replays a recording of the Go
employeeChanged fetcher's cursors, events, and error classes. The example
integration tests require a real `dexcli dev` server and use local provider
stand-ins; live BambooHR behavior, including live polling, has not been
verified.
