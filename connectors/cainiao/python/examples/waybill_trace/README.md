# Cainiao electronic-waybill trace Flow

The [Flow](flow.py) queries one carrier `cp_code` and Cainiao electronic
waybill code through `queryWaybillTrace`, then persists its bounded events.
The static Dex Web Connections name is `cainiao`; optional provider rejection
and invalid-response branches fail the Flow.

Configure the three credentials using the [connector instructions](../../README.md).
Start `dexcli dev`, generate strict FDG 2.0 with
`dexcli visualize examples/waybill_trace/flow.py --python .venv/bin/python --schema-version 2.0 --json
--out build/waybill-trace`, then run
`python -m examples.waybill_trace.worker` from `connectors/cainiao/python`.
Start `CainiaoWaybillTracePython` with
`{"carrierCode":"YTO","waybillCode":"12345"}` using a real authorized
electronic waybill. Set `DEX_FLOW_SERVICE_ADDRESS` for a nondefault Dex server.

`integration_test.py` uses inert credentials and a local provider stand-in to
verify the real Dex Worker, Client, persistence, and a 503 retry. Live TOP
merchant authorization and trace delivery remain unverified.
