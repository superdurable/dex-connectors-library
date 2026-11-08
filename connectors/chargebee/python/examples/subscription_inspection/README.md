# Chargebee Python subscription inspection

This runnable Flow calls `getSubscription` through the static `chargebee` connection, persists the selected subscription status, and completes. Optional branches are unwired and fail the Flow when selected. Configure `api_key` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/chargebee/python`, run `uv run python -m examples.subscription_inspection.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `ChargebeeSubscriptionInspectionPython` with a unique Flow ID and input `{"subscriptionId":"sub_123456"}`. Replace the sample subscription ID with one returned by Chargebee.
