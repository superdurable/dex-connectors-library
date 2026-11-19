# Amazon DynamoDB connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/amazon/dynamodb/go@v0.2.0`.

`getItem` reads one table item by its complete primary key through the [DynamoDB GetItem API](https://docs.aws.amazon.com/amazondynamodb/latest/APIReference/API_GetItem.html). Supply `tableName`, a `key` map with one partition attribute and an optional sort attribute, and `consistentRead` to request a strongly consistent read. Each key attribute is a DynamoDB AttributeValue object with one nonempty `S`, `N`, or base64 `B` value. The `found` branch returns `exists: true` and the raw `Item` AttributeValue map. The optional `missing` branch returns `exists: false` and an empty item map. AWS throttling, 429, 408, 5xx, and transport errors retry. Definitive provider rejection, invalid responses, and invalid input take optional branches. AWS Signature Version 4 signs the JSON protocol POST.

| Operation | API action | Happy branch | Retry tier |
| --- | --- | --- | --- |
| `getItem` | [GetItem](https://docs.aws.amazon.com/amazondynamodb/latest/APIReference/API_GetItem.html) | `found` | `safe` |
| `putItem` | [PutItem](https://docs.aws.amazon.com/amazondynamodb/latest/APIReference/API_PutItem.html) | `written` | `safe` |
| `updateItem` | [UpdateItem](https://docs.aws.amazon.com/amazondynamodb/latest/APIReference/API_UpdateItem.html) | `updated` | `safe` |
| `deleteItem` | [DeleteItem](https://docs.aws.amazon.com/amazondynamodb/latest/APIReference/API_DeleteItem.html) | `deleted` | `safe` |
| `scanItems` | [Scan](https://docs.aws.amazon.com/amazondynamodb/latest/APIReference/API_Scan.html) | `scanned` | `safe` |

`putItem` is a complete replacement with supplied key attributes and can create an item. `updateItem` builds only absolute `SET` assignments, never increments or appends, and can create an item if absent. `deleteItem` removes one item by complete key. Their results confirm acceptance without copying item values into Flow state. `scanItems` accepts an optional limit from 1 to 1000 and `exclusiveStartKey` from a prior page; blank starts at the table beginning. It returns `lastEvaluatedKey` for continuation. All writes are repeat-safe because replacement, absolute update, and deletion converge to the same item state.

In the [DynamoDB Console](https://console.aws.amazon.com/dynamodb), open **Tables**, choose the table and note its name, partition key, optional sort key, and console Region. In the [IAM Console](https://console.aws.amazon.com/iam), open **Users > selected user > Security credentials > Access keys > Create access key**. Grant only the needed `dynamodb:GetItem`, `dynamodb:PutItem`, `dynamodb:UpdateItem`, `dynamodb:DeleteItem`, and `dynamodb:Scan` actions on the table ARN. Enter the access key ID and one-time secret access key in their secret fields. For temporary credentials also enter the STS session token. Leave `endpoint` blank for the regional AWS endpoint; leave `timeout` and `maxResponseBytes` blank for the displayed defaults. Never put credentials in Flow input or logs.

The [Go](go/examples/item-lookup/README.md) example includes item lookup and replacement Flows. The [Python](python/examples/item_lookup/README.md) and [TypeScript](typescript/examples/item-lookup/README.md) examples continue to cover `getItem`. The Go-recorded [conformance scenarios](../../../conformance/connectors/amazon/dynamodb/scenarios.json) cover every Go operation's branches, retry classifications, and requests. Provider fixtures and real Dex Worker tests are included. Live AWS authorization, table reads, and writes have not been verified.
