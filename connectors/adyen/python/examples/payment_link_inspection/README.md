# Adyen Python payment link inspection

This runnable Flow calls `getPaymentLink` through the static `adyen` connection, persists the selected payment link status, and completes. Optional branches are unwired and fail the Flow when selected. Configure `api_key` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/adyen/python`, run `uv run python -m examples.payment_link_inspection.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `AdyenPaymentLinkInspectionPython` with a unique Flow ID and input `{"paymentLinkId":"PL50C5F751CED39G71"}`. Replace the sample payment link ID with one returned by Adyen.
