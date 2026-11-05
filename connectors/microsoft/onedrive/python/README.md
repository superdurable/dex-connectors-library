# Microsoft OneDrive and SharePoint for Python

This package implements `searchFiles`, `getFile`, `readFileText`, `uploadFile`, and `createFolder` from the [OneDrive connector](../README.md). It uses the generated Microsoft delegated OAuth refresh and app-only token grant declarations. Token exchange and Graph calls run through the configured HTTP transport; access tokens and download links never enter Flow inputs or outputs.

Use `new_project_connection(project, "onedrive-files")` after loading the project configuration. The runnable [getFile example](examples/get_file) includes a Worker entrypoint, a Flow start command, and a real Dex integration test. The same static connection name must be configured in Dex Web. The parent [authorization guide](../README.md#authorization) explains delegated scopes, app registration, and app-only access.

From this directory, run `uv sync --locked`, `uv run ruff check src examples tests`, `uv run mypy --strict src examples tests`, and `uv run pytest tests`. To run the integration test, start `dexcli dev -dex-port 40690 -web-port 40691 -open=false` and set `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:40690`; the test uses an inert Graph stand-in through the real Worker and Client. Live Microsoft authorization remains unverified.
