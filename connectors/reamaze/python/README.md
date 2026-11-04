# Re:amaze connector for Python

`dex-connector-reamaze` provides six async Re:amaze operations for Dex Flows: search and read conversations, create and update conversations, add a reply or internal note, and find a contact by email. It uses the `dex_connectors.reamaze` import package and requires Python 3.11 or newer.

Install `dex-connector-reamaze==0.1.0` after release. The
[contact lookup example](examples/find_contact/README.md) contains the Worker,
Client start command, and a real Dex integration test.

Configure `brand` as the lowercase subdomain in `https://{brand}.reamaze.io`; set `email` to the staff login address and store the user's API token in the secret `api_token` credential field. Re:amaze calls use HTTP Basic on the brand's `/api/v1` endpoint. Each call reads current credentials, and the token is kept out of Flow inputs and results.

The write operations preserve Go's branch identities and Dex retry behavior. `createConversation` and `replyToConversation` use a single-dispatch heartbeat fence and read-back to avoid resending a request whose outcome is unknown. `updateConversation` reads the conversation before setting absolute status, assignee, and tag values. An unwired optional branch fails the Flow.

For `findContactByEmail`, `notFound` means a successful contact search returned no exact email match. A 404 response from `/contacts` selects `providerRejected`.

The generated `dex-connector.json`, models, connection bindings, Step factories, and mocks come from `connector.yaml`. The provider client and operation invocations are hand-written. Provider replay uses the Go-recorded conformance scenarios. No live Re:amaze authorization or provider request has been verified for this Python package.
