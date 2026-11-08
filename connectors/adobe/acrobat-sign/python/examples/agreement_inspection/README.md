# Adobe Acrobat Sign Python agreement inspection

This runnable Flow calls `getAgreement` through the static `adobe-acrobat-sign` connection, persists the selected status, and completes. Optional branches are unwired and fail the Flow when selected. Configure `integration_key` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/adobe/acrobat-sign/python`, run `uv run python -m examples.agreement_inspection.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `AcrobatSignAgreementInspectionPython` with a unique Flow ID and input `{"agreementId":"3AAABLblqZNOTREALAGREEMENTID5_BjiH"}`. Replace the synthetic ID with one returned by Adobe Acrobat Sign.
