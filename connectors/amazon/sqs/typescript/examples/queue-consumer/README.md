# SQS queue consumer, TypeScript

`AmazonSQSQueueConsumerTypeScript` starts with `{ "queueUrl": "https://sqs.eu-west-1.amazonaws.com/123456789012/orders" }`, or `{}` when `defaultQueueUrl` is configured. It receives one message, checks for an order body such as `{ "orderId": "1042", "event": "paid" }`, and deletes it after validation. An invalid body is released by setting visibility to zero so the queue's redrive policy can handle repeats. The `empty` branch completes without processing. Every provider operation uses the static `amazon-sqs-orders` connection, and all other optional branches fail the Flow.

Configure the connection in Dex Web as described in the [TypeScript package README](../../README.md), then run `npm ci` from `connectors/amazon/sqs/typescript`. Start `dexcli dev` and run `npm run example:queue-consumer` to register the Worker. Start the Flow in Dex Web with the safe queue selection. The Worker loads the project connection and resolves secrets only at provider invocation time.

For a credential-free Dex integration check, run `npm run integration:queue-consumer` against a running `dexcli dev` server. It uses the same Flow with scripted provider results and asserts the processed and released paths through a real Dex Worker, Client, and persistence boundary. Set `DEX_FLOW_SERVICE_ADDRESS` when Dex runs on a nondefault port.

Generate the strict FDG 2.0 visualization with Dex CLI 1.33.0 or newer. From this package directory, build the package and expose its local manifest to the CLI:

```sh
npm run build
mkdir -p node_modules/@superdurable
test -e node_modules/@superdurable/connector-amazon-sqs || ln -s ../.. node_modules/@superdurable/connector-amazon-sqs
dexcli visualize examples/queue-consumer/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/amazon-sqs-ts-queue-consumer
```

The symlink is only for this checkout's source analyzer; a published installation already has the package under `node_modules`. Provider authorization and live SQS behavior remain unverified for this TypeScript example.
