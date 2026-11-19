# Hesi department directory Flow

Install with `uv sync --locked --project connectors/hesi/python`. Configure the
`hesi` connection in Dex Web Connections using the credential and enterprise
origin instructions in the [connector README](../../README.md). From the Python
package directory, run `uv run python -m examples.department_directory.worker`
with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev -open=false`. Start
`HesiDepartmentDirectory` with `{"start":0,"count":20}`. Only `listed` is
wired; other outcomes fail the Flow for inspection.

The integration test runs this Flow against real Dex with a fake provider,
including the retry path. Live Hesi credentials and department data remain
unverified.
