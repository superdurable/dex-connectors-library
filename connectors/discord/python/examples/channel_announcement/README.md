# Discord channel announcement (Python)

This runnable Flow sends one bot message through the static `discord-announcements` connection. It stores the resulting message as a safe Attribute. The happy `created` branch completes; an unwired optional rejection fails the Flow. The connector sends `allowed_mentions.parse=[]`, so the sample `@everyone` text makes no mention.

Configure the connection in Dex Web using the [Discord bot setup guide](../../../README.md), then run `uv sync` from `connectors/discord/python`. Start `dexcli dev -dex-port 22041 -web-port 22042 -open=false` in another terminal, and set `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:22041`. Run `uv run python -m examples.channel_announcement.worker`, then in another terminal run `uv run python -m examples.channel_announcement.start discord-announcement-001 examples/channel_announcement/sample-input.json`. Use a unique Flow ID for every run and replace the sample channel ID with the ID from your server.

The integration test uses an inert token and a fake Discord HTTP server, but it runs the example through a real Dex Worker and Client. Live Discord provider authorization and message delivery remain unverified.
