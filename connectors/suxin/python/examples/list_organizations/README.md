# Suxin listOrganizations example

Run `uv run python -m examples.list_organizations.worker` after configuring the static `suxin` connection in Dex Web. Run `uv run pytest examples/list_organizations/integration_test.py` against a local Dex stack. The integration test uses fake provider responses; live provider behavior remains unverified.
