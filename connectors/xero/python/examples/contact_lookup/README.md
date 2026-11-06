# Xero Python contact lookup

`ContactLookupFlow` uses the static `xero-books` connection and the generated `findContactByEmail` Step factory. The found branch completes with the contact; an unwired optional branch fails the Flow. Configure the Xero Custom Connection or OAuth web app in Dex Web **Connections** as described in the [connector setup](../../../README.md#xero-setup). This example has no Step configuration fields.

Run `dexcli visualize examples/contact_lookup/flow.py --schema-version 2.0 --json --python .venv/bin/python --out /tmp/xero-python-contact-lookup` from `connectors/xero/python` to generate strict FDG 2.0. Set `DEX_FLOW_SERVICE_ADDRESS` and `DEX_CONNECTOR_CONFIG_FILE` for the local Dex project, then run `uv run python -m examples.contact_lookup.main`. Start `XeroFindContactByEmailPython` with a unique Flow ID and JSON input such as `{"emailAddress":"accounts@example.com"}`.

The integration test starts a real Worker and Client against `dexcli dev`, with a local Xero-compatible stand-in bound to an OS-assigned free port. Live Xero authorization and API behavior remain unverified.
