# Hiver inbox page Flow

This runnable Python Flow lists one page of Hiver shared inboxes through the static `hiver-support` connection. Configure the Hiver API key in Dex Web Connections and run `python -m examples.inbox_page.main` from `connectors/hiver/python` after starting `dexcli dev`. The included `sample-input.json` selects the first page. The integration test uses a local provider stand-in and a real Dex Worker and Client; it does not prove live Hiver authorization.
