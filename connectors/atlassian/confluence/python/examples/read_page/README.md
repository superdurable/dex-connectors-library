# Read a Confluence page

This Flow reads one page through the static `confluence-pages` connection and returns bounded Markdown or plain text. Configure that connection in Dex Web using the [connector setup guide](../../../README.md); no token goes in Flow input. For `pageId`, open a page on your Confluence site and copy the numeric ID after `/pages/` in its URL. Use `bodyFormat` `markdown` or `plainText`; blank uses Markdown.

Generate strict FDG 2.0 and start the local Dex stack with free ports in your assigned range:

```sh
dexcli visualize ./examples/read_page/flow.py --python .venv/bin/python --schema-version 2.0 --json --out ./build/read-page
dexcli dev -open=false -dex-port 46801 -web-port 46802
```

In another terminal, set `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:46801` and load the project connection, then run:

```sh
uv run python -m examples.read_page.worker
uv run python -m examples.read_page.start "confluence-read-$(date +%s)" examples/read_page/input.example.json
```

The integration test uses the same Flow against a local provider stand-in and a real Dex Worker and Client. Live Atlassian OAuth, scope consent, site selection, and API responses remain unverified.
