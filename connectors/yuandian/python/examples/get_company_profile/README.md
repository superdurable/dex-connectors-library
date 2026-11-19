# Yuandian getCompanyProfile example

Run `uv run python -m examples.get_company_profile.worker` after configuring the static `yuandian` connection in Dex Web. Run `uv run pytest examples/get_company_profile/integration_test.py` against a local Dex stack. The integration test uses fake provider responses; live provider behavior remains unverified.
