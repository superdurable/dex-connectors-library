# Gmail connector for TypeScript

Install the connector in a Dex application:

```sh
npm install @superdurable/connector-google-gmail @superdurable/dex
```

This package provides typed `getMessage`, `sendMessage`, and `replyToMessage` operations. Its operation identities, branches, retry policy, authorization fields, and mock cases mirror [the manifest](https://github.com/superdurable/dex-connectors-library/blob/main/connectors/google/gmail/connector.yaml). Gmail Triggers and the thread-reply Trigger runner currently belong to the Go connector.

## Configure a connection

In Dex Web **Connections**, create a Google Gmail connection with the static name used by the application's `ConnectionName` constant. The application loads project settings through the Connector SDK and resolves credentials during each Step attempt. Refreshes update encrypted project storage or Dex Web's private local `connections.json`; tokens and service-account keys must never be put in Flow input or persisted state.

For personal Gmail or ordinary Workspace accounts, select **Google OAuth**. In [Google Cloud Credentials](https://console.cloud.google.com/apis/credentials), create a Web application, enable the Gmail API, and add the exact redirect URI shown by Dex Web. Configure the consent audience, test users when the app is in Testing, and the scopes shown by Dex Web. Paste the client ID and client secret into the connection form; Dex Web derives the verified primary email and stores the access and refresh tokens after consent.

For administrator-managed Workspace delegation, use [Google Cloud service accounts](https://console.cloud.google.com/iam-admin/serviceaccounts) to create a service account and download its JSON key. In [Workspace domain-wide delegation](https://admin.google.com/ac/owl/domainwidedelegation), authorize that account's OAuth client ID for the Gmail readonly and send scopes. Put the JSON key and one managed mailbox in the connection form. The connector mints short-lived delegated tokens and uses that mailbox as the sender.

`sendMessage` and `replyToMessage` claim a Dex heartbeat checkpoint before POST and search Sent Mail under a run-specific RFC `Message-ID` after an ambiguous send. A confirmed match returns `sent`; a miss retries read-back within the manifest budget without another POST. A conclusive 429 or 503 can permit another attempt after its own read-back; delayed Sent Mail indexing after a 503 can still cause duplicate delivery. An accepted 2xx never resends merely because its response body is unusable.

## Run the example

[The Gmail Flow examples](https://github.com/superdurable/dex-connectors-library/tree/main/connectors/google/gmail/typescript/examples/gmail-flow) contain one Flow per operation, one Worker, a read starter, and a real Dex integration test. Their static connection name is `gmail-inbox`. The Worker setup below is from the [runnable entrypoint](https://github.com/superdurable/dex-connectors-library/blob/main/connectors/google/gmail/typescript/examples/gmail-flow/main.ts):

```ts
const project = await loadFromEnvironment();
const connection = newProjectConnection(project, ConnectionName);
const registry = new Registry([new GmailReadFlow(connection), new GmailSendFlow(connection), new GmailReplyFlow(connection)]);
```

Run `npm run example:worker` after configuring the named connection, then `npm run example:start -- GMAIL_MESSAGE_ID` to start a read. `npm run test:integration` exercises read, send, reply, retry, optional branch failure, and the send checkpoint with a fake provider on a real Dex server. No live Google authorization or delivery has been tested.

Dex CLI v1.28.0 rendered all three example Flows as valid FDG 2.0 graphs with no diagnostics. The published package includes the manifest-derived `dex-connector.json` marker, which supplies connector identity, named branches, and Dex Web connection configuration metadata.

Flows that route from a local Step into a connector Step need Dex CLI v1.28.1 for strict FDG visualization ([issue #362](https://github.com/superdurable/dex-connectors-library/issues/362)). The three examples here start at connector Steps and pass with v1.28.0.
