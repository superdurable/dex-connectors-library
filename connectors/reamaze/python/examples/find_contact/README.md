# Re:amaze contact lookup Flow

This Flow starts with an email address and uses `findContactByEmail` through the static `reamaze-support` connection. Only the `found` branch is wired. A missing contact, definite provider rejection, or invalid response fails the Flow; rate limits and availability failures use the operation's Dex retry policy.

Start `dexcli dev` on free local ports and configure `reamaze / reamaze-support` in Dex Web. Enter the brand subdomain from Re:amaze **Settings > Brands**, the staff login email, and that user's API token from **Settings > Developer > API Token**. Point `DEX_CONNECTOR_CONFIG_FILE` at the Dex Web project configuration and `DEX_FLOW_SERVICE_ADDRESS` at the local Dex gRPC address.

From `connectors/reamaze/python`, run `uv sync --locked`, then `uv run python -m examples.find_contact.main`. In another terminal, run `uv run python -m examples.find_contact.start reamaze-contact-001 examples/find_contact/sample-input.json` with a fresh Flow ID. The start input contains no credential. `flow.py` registers the contact result Attribute so Dex Web can display it.

The integration test uses a free local provider port and the same Flow with a real Dex Worker and Client. Run it with `DEX_FLOW_SERVICE_ADDRESS` set to the address of `dexcli dev`.
