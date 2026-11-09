# Amazon DynamoDB Python package

`Client.get_item()` binds the signed `getItem` Query. `GetItemInput` contains `table_name`, the complete DynamoDB `key` AttributeValue map, and `consistent_read`. The `found` output contains `ItemResult(exists=True, item=...)`; `missing` has an empty item map. See the [connector guide](../README.md) for configuration and IAM permissions. The [runnable Flow](examples/item_lookup/README.md) exercises a real Dex Worker with a local provider fixture. Live AWS access has not been verified.
