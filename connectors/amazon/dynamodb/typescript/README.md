# Amazon DynamoDB TypeScript package

`AmazonDynamoDBClient.getItem()` binds the signed `getItem` Query. `GetItemInput` contains `tableName`, the complete DynamoDB `key` AttributeValue map, and `consistentRead`. The `found` output contains `{ exists: true, item }`; `missing` has an empty item map. See the [connector guide](../README.md) for configuration and IAM permissions. The [runnable Flow](examples/item-lookup/README.md) exercises a real Dex Worker with a local provider fixture. Live AWS access has not been verified.
