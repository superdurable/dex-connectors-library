# Help Scout conversation search example

Run `uv run python -m examples.conversation_search.worker` from `connectors/helpscout/python/` after creating a `helpscout` connection in Dex Web. The static connection name is `helpscout`. Start the `HelpScoutConversationSearch` Flow with a `SearchConversationsInput`, for example the checked-in [`input.example.json`](input.example.json). The Flow stores and returns one page of conversations. Its only wired branch is `searched`; an optional branch fails the Flow when selected.

The checked-in `integration/conversation_search.py` runs this same Flow against a real local Dex Worker and Client with generated provider mocks. Start `dexcli dev` on unused ports, set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address, then run `uv run pytest integration/conversation_search.py -q`.

Generate and validate the strict Flow Definition Graph 2.0 from this package directory with:

```sh
dexcli visualize ./examples/conversation_search/flow.py \
  --python .venv/bin/python --schema-version 2.0 --lint app \
  --json --out /tmp/helpscout-python-fdg
```
