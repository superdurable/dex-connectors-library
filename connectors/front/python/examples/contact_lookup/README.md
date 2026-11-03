# Front contact lookup Flow

This Flow uses the static `front-support` connection to find the Front contact
that owns one email handle. Run its Worker with `python -m
examples.contact_lookup.main` after configuring the connection through Dex Web.
The connection holds an API token; the Flow input is only an email address.

Start `dexcli dev` and configure `front / front-support` in Dex Web **Connections**.
Create the Front API token using the authorization guide in
[`connector.yaml`](../../../connector.yaml). In Front, an admin opens
<https://app.frontapp.com/> > **Settings > Developers > API Tokens**,
creates a token with contact read access, and copies its secret into Dex Web.
Leave the response byte limit blank for the displayed default. Set
`DEX_CONNECTOR_CONFIG_FILE` to the private `connections.json` exported by that
project, then run:

```sh
uv sync --locked
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  uv run python -m examples.contact_lookup.main
```

The Worker registration uses the real application source in [`main.py`](main.py).
Start `FrontContactLookupPython` with `sample-input.json` using a Dex Client.
Only the `found` branch is wired; a missing contact, provider rejection,
invalid response, or local defect fails the Flow. `integration_test.py` runs
this Flow through a real Dex Worker and Client against a local Front stand-in.
Live Front authorization and provider behavior remain unverified.
