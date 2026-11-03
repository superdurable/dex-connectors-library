# Ticket triage example

This Flow reads one Gorgias ticket and its latest messages through the static `gorgias` connection. Create the connection in Dex Web using the Gorgias domain, user email, and API key described in the [connector README](../../../README.md).

Start a local Dex stack, set `DEX_FLOW_SERVICE_ADDRESS` to its Dex gRPC address, then run `uv run python -m examples.triage_issue.worker` from `connectors/gorgias/python`. Start a Flow in Dex Web using type `GorgiasTriageIssuePython` and the checked-in `sample-input.json`. The ticket and messages appear as the Flow output.

The integration test uses a local provider stand-in to verify this same Flow with a real Dex Worker and Client. Live Gorgias behavior requires a user-controlled connection and has not been verified locally.
