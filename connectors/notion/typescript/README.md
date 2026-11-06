# Notion TypeScript connector

`@superdurable/connector-notion@0.1.0` implements Notion's `search`, `queryDatabase`, and `getPage` Queries and `createPage` and `updatePageProperties` Mutations. The manifest generates operation models, Step factories, connection bindings, mock cases, and `dex-connector.json`. Import scripted mocks from `@superdurable/connector-notion/mock`.

## Configure a connection

Create a Notion internal connection at [Notion's developer portal](https://app.notion.com/developers/connections) under **Build → Internal connections**. Enable **Read content**, **Update content**, and **Insert content**, then share each database or page with the connection. Save its API token in the secret `api_token` field in Dex Web **Connections**. Keep the token out of Flow inputs, Attributes, and logs. `endpoint` and `maxResponseBytes` may be left blank for the manifest defaults.

A database contains one or more data sources. `queryDatabase` and `createPage` accept either a data source ID or a database ID; a database ID triggers one lookup and requires exactly one data source. A page ID accepts dashed or compact hex and a Notion page URL. Search is title-only and may lag recently created pages.

## Run the example

The [search-pages example](examples/search-pages/README.md) loads the static `notion` connection and runs the generated `newSearchStep` through a Dex Worker. It persists the request and result as Attributes. The optional `noMatch` and rejection branches remain unwired, so selecting one fails the Flow. The API token stays in the connection credential source.

## Failure behavior and verification

Reads retry transient transport, 408, 409, 429, 529, and server errors. `updatePageProperties` repeats absolute values safely and retries transient failures. `createPage` uses the Connector SDK single-dispatch fence; an ambiguous write without a confirmable page ID selects `uncertain`, while a known accepted ID can be read back. Conclusive provider rejections select operation branches. Notion error message text is not exposed.

Run `npm ci && npm run check`. With a local `dexcli dev` service, set `DEX_FLOW_SERVICE_ADDRESS` and run `npm run test:integration`. The conformance suite replays Go-recorded provider requests and results. Live Notion authorization and response behavior remain unverified.
