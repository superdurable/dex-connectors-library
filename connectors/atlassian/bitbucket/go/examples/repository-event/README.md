# Bitbucket repository event example

`BitbucketRepositoryEvent` starts one Flow for each HMAC-verified Bitbucket repository or workspace webhook. The static connection is `bitbucket`, and the Trigger binding is `repository-events`. The Flow stores safe resource identifiers and completes. A repeated Bitbucket delivery with the same request UUID uses the same Trigger event ID and Flow ID.

## Generate strict FDG 2.0

From `connectors/atlassian/bitbucket/go`:

```sh
dexcli visualize ./examples/repository-event/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/bitbucket-repository-event-fdg
```

The JSON should report `valid: true`.

## Configure and run

Create the `bitbucket` connection and `repository-events` binding in Dex Web. The connection keeps its workspace access token for API operations. At [Bitbucket](https://bitbucket.org/), open **Workspace > Repository > Repository settings > Webhooks > Add webhook** or **Workspace > Workspace settings > Webhooks**. Point the webhook to a publicly reachable HTTPS URL that forwards POST requests to the application's listener, select desired events, and set a secret. Pass the same secret to the example process through `BITBUCKET_WEBHOOK_SECRET`. The default local listener is `127.0.0.1:9212`; set `BITBUCKET_WEBHOOK_LISTEN_ADDRESS` to change it. Keep the HTTPS forwarding layer and environment secret under your control. The secret is never Flow input.

Start Dex with `dexcli dev -open=false -dex-port 9210 -web-port 9211`. From `connectors/atlassian/bitbucket/go`, start the Worker and webhook listener:

```sh
BITBUCKET_WEBHOOK_SECRET="<secret from your secret store>" DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:9210 go run ./examples/repository-event
```

The endpoint records a verified delivery in the binding's durable project inbox before acknowledging it. Its Trigger target starts a Flow with a stable ID based on `X-Request-UUID` and `X-Event-Key`. The example accepts all supported events from the connection; an application can narrow them by repository or event key in its Trigger filter. An unset secret or stopped Trigger gives Bitbucket a retryable response.

The in-process HMAC tests and the real Dex integration test use test credentials and a local webhook request. With `dexcli dev` running on port 9210, run:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:9210 GOWORK=off go test -tags=integration ./examples/repository-event/flow -run '^TestSignedRepositoryWebhookStartsFlowWithRealDex$' -count=1 -v
```

No live Bitbucket webhook was configured for this example. The real provider's delivery and permissions remain to be verified with a user-controlled Bitbucket repository.
