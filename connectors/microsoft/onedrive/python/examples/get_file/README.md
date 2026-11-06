# Read one OneDrive file

This Flow uses the static `onedrive-files` connection and the `getFile` Query. Configure the connection in Dex Web with the [Microsoft authorization guide](../../../README.md#authorization), then run `uv run python -m examples.get_file.worker` from `python/`. Start a Flow with `uv run python -m examples.get_file.start onedrive-file-1 examples/get_file/input.example.json`. Set `DEX_FLOW_SERVICE_ADDRESS` when Dex uses a nondefault port.

The Flow stores the returned metadata in a Dex Attribute. An unwired optional branch fails the Flow. The integration test runs this Flow through a real Dex Worker and Client with an inert Graph stand-in; live Microsoft authorization remains unverified.
