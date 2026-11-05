# Hiver conversation claim Flow

This runnable Flow uses all six Hiver Python operations: list the shared inboxes, list the selected inbox's conversations, read the chosen conversation and its message IDs, set its status, add an internal note, and prepare a shared reply draft. A Hiver user reviews and sends the draft; this Flow does not send mail to a customer.

The Flow uses the static `hiver-support` connection. Start `dexcli dev`, configure that connection's `api_key` in Dex Web Connections, and run `python -m examples.claim_conversation.main` from `connectors/hiver/python`. The included `sample-input.json` uses example IDs; replace them with IDs from your own Hiver account before starting a Flow. The integration test exercises this exact Flow through a real Dex Worker and Client with a local provider stand-in, bound to an operating system assigned port. Live Hiver behavior remains unverified.
