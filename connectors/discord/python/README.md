# Discord Python connector

`dex-connector-discord` binds four Discord bot operations to Dex Steps: read a channel, list a bounded page of messages, send a nonce-deduplicated message, and add a reaction. Every mention is disabled when sending a message. The Go connector remains the source of the provider contract.

Install `dex-connector-discord==0.1.0` alongside `dex-python-sdk>=1.28.0`. Configure a `discord` connection with a bot token through Dex Web Connections, then use `new_project_connection(project, "discord-announcements")` in a Worker. The [runnable channel announcement](examples/channel_announcement/README.md) demonstrates the static connection name and Step factories.

Run `uv sync`, then `uv run pytest`. Provider scenarios replay recorded Go behavior, including retries, response validation, and a stable nonce. Live Discord authorization and API behavior are unverified in this package.
