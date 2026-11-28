# Siliconflow Python connector

This package implements the Query declared in [`../connector.yaml`](../connector.yaml). Run the checked-in [`provider_query` Flow](examples/provider_query/) with a Dex connection named `siliconflow`. The integration test uses a synthetic provider and verifies 503 retry and persisted output against a real Dex Worker and Client. Live provider authorization has not been exercised.

Model IDs must be unique and fit in 512 UTF-8 bytes. Missing, null and empty object tags are omitted from the persisted output.
