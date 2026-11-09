# Telegram Python message send

The `TelegramPythonMessageSend` Flow uses the static `telegram` Dex Web
connection. Configure the BotFather token in Dex credentials, then run
`uv run python -m examples.worker` from `python/` with `DEX_PROJECT_*` and
`DEX_FLOW_SERVICE_ADDRESS` set. Start it with:

```json
{"chatId":"@meridian_team","text":"Hello from Dex"}
```

Generate strict FDG 2.0 with
`dexcli visualize examples/message_send/flow.py --schema-version 2.0 --json --python .venv/bin/python --out /tmp/telegram-python-send-fdg --lint app`.
The real Dex mock integration test covers the checked-in Flow. Live Telegram
authorization and delivery remain unverified.
