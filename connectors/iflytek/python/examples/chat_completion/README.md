# iFLYTEK Spark chat completion Flow

This runnable Python Flow generates one bounded answer through the static `iflytek` connection and persists only assistant content, model ID, and token counts. Configure APIPassword in Dex Web Connections using the connector authorization guide, then run `python -m examples.chat_completion.worker` from this package directory with `DEX_FLOW_SERVICE_ADDRESS` set to the Dex Flow service.

The integration test uses a fake HTTP provider and real Dex Worker/Client. It verifies a 503 retry and persisted completion. Run strict FDG 2.0 generation with the package README command. Live authorization remains unverified.
