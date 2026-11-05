# Form catalog example

Run `dexcli dev` with ports of your choice, create a Dex Web connection named `typeform-forms` with a Typeform personal access token, then run `uv run python -m examples.form_catalog.worker` from `connectors/typeform/python`. Set `DEX_FLOW_SERVICE_ADDRESS` to the dev server's gRPC address if it is not `localhost:8801`. In Dex Web, start `TypeformFormCatalogPython` with `{"workspaceId":"","page":1}` to list the first page. An empty workspace ID lists all workspaces.

The example's integration test uses a provider stand-in and a real Dex Worker, Client, and local `dexcli dev` service. The live Typeform API and authorization flow remain unverified.
