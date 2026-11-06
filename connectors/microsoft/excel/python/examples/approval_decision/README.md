# Excel approval decision

This Flow reads a policy table and summary range, writes a literal summary row, and appends a decision row by `RequestId` once. It uses the static Dex connection name `approvals`.

Configure the Microsoft Excel OAuth connection in Dex Web using the connector's authorization guide. Choose a workbook with `ApprovalPolicy` and `Decisions` tables, a `Summary` worksheet, and a two-cell summary range such as `A1:B1`. Run `dexcli dev` and start the worker with `python -m examples.approval_decision.worker` from `python/`, setting `DEX_FLOW_SERVICE_ADDRESS` to the dev server's address. The checked-in integration test uses inert credentials and a provider stand-in with a real Dex Worker and Client.
