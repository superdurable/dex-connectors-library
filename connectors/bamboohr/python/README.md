# BambooHR Python connector

`dex-connector-bamboohr` provides typed Dex Steps for `getEmployee`,
`findEmployeeByEmail`, `listEmployeeChanges`, `listTimeOffRequests`,
`updateEmployee`, and `addEmployee`. Each uses the branches and defaults in
`connector.yaml`. `addEmployee` uses sync Execute and a persisted dispatch
fence so an ambiguous creation is never sent again. All other optional branches
fail an example Flow unless the application wires them for a business reaction.

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
`uv run --locked mypy src examples/get_employee tests`. The conformance test
replays the checked-in Go provider recording. The example integration test
requires a real `dexcli dev` server and uses an inert local provider stand-in;
live BambooHR behavior has not been verified.
