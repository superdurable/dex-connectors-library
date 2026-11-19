# Qince listDepartments example

Run `uv run python -m examples.list_departments.worker` after configuring the static `qince` connection in Dex Web. Run `uv run pytest examples/list_departments/integration_test.py` against a local Dex stack. The integration test uses fake provider responses; live provider behavior remains unverified.
