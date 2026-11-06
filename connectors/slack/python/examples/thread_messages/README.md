# Slack thread messages Python example

Create a Dex Web connection named `slack-thread-messages` using the Slack OAuth setup in the [connector guide](../../../README.md). Set `DEX_FLOW_SERVICE_ADDRESS` to a running `dexcli dev` gRPC address and run `python -m examples.thread_messages.main` from `connectors/slack/python`. Start `SlackThreadMessagesPython` with `{"channelId":"C0123456789","text":"Hello from Dex"}`. The Flow posts one channel message, reads its thread, stores the page, and completes. Unwired optional branches fail the Flow.

The integration test runs this same Flow through a real Dex Worker and Client with a local HTTP transport standing in for Slack. Live Slack OAuth and Web API behavior remain unverified.
