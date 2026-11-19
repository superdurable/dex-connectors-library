# Stored summary example

Run `npm run example:summarize` from `connectors/openai/typescript` after configuring the `openai-api` connection in Dex Web. The Worker registers `OpenAISummarizeTextTS`, validates text up to 64 KiB, creates one stored OpenAI Response, and returns its ID, summary, model, status, and usage. The Step uses synchronous Execute durability and a heartbeat checkpoint to prevent duplicate billed create calls. A rejected or uncertain response fails the Flow through its unwired optional branch.

Set `DEX_FLOW_SERVICE_ADDRESS` to the Dex gRPC address. The integration test uses a local provider on an operating-system-assigned port and a real `dexcli dev` Worker and Client path. Live OpenAI provider behavior is unverified.
