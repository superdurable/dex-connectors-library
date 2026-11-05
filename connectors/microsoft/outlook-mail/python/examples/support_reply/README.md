# Outlook Mail support reply (Python)

This Flow uses the static `outlook-support-mailbox` connection. It searches the Inbox for a customer's message, reads the newest match, sends one threaded reply, marks the source read, and moves it to Archive. If no message matches, it sends a new message. All six operations run through their operation-specific Steps.

Create the connection in Dex Web using the [Microsoft authorization guide](../../../README.md), then from `python/` run `uv sync --locked`, `uv run python -m examples.support_reply.worker`, and `uv run python -m examples.support_reply.start outlook-support-1 examples/support_reply/input.example.json`. Set `DEX_FLOW_SERVICE_ADDRESS` for a nondefault Dex port. The integration test uses an inert provider fake with a real Dex Worker and Client; live Microsoft authorization remains unverified.
