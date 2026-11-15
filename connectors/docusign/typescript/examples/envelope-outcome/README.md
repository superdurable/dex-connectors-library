# DocuSign envelope outcome example

This TypeScript example is the smallest consuming application of the `envelopeEventReceived` Trigger. One `EnvelopeOutcomeFlow` runs per signature request, with the Flow ID `docusign-signing-` plus the request ID:

1. `RecordSigningRequest` validates the request and stores it in the `docusign-signing-state` Attribute.
2. `SendEnvelope` runs `createEnvelopeFromTemplate`, tagging the envelope with the hidden custom field `dexSigningRequestId`. Unwired `providerRejected`, `uncertain`, and `defect` branches fail the Flow.
3. `RecordEnvelopeSent` stores the envelope ID and waits with status `waitingForSignature`.
4. The Trigger delivers each verified Connect event to the `receiveEnvelopeEvent` RPC. The target's `acceptEnvelopeEvent` filter consumes an event without a well-formed `dexSigningRequestId`, and `resolveFlowId` finds the Flow from that field. The RPC ignores a repeated event ID, keeps an event that overtook the recorded envelope pending, ignores an event for another envelope or after the outcome, and otherwise schedules `RecordEnvelopeOutcome`.
5. `RecordEnvelopeOutcome` completes the Flow as `completed`, `declined`, or `voided`.

The Go [envelope-signing example](../../../go/examples/envelope-signing/README.md) adds the status-poll fallback, the signing deadline, and the signed document download.

`app.ts` serves the Connect endpoint at `/docusign/connect` and a readiness check at `/readyz` on `CONNECT_BIND_ADDRESS` (default `127.0.0.1:8862`) before the Worker starts, so deliveries are recorded in the binding's durable project inbox even while Dex is unreachable. Configure the `docusign-esignature` connection, including `connect_hmac_key`, and the `envelope-outcomes` binding in Dex Web as the [connector README](../../../README.md#connect-configuration) describes; the binding has no configuration units and saves as `{}`, which records all three outcomes. Expose the endpoint through an HTTPS tunnel as the Connect configuration's URL to Publish, then run:

```sh
npm run build
DEX_CONNECTOR_CONFIG_FILE=<path to connections.json> DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npx tsx examples/envelope-outcome/main.ts
```

Start `DocuSignEnvelopeOutcomeFlow` with the Flow ID `docusign-signing-opp-123` and the input `{"requestId": "opp-123", "templateId": "<template GUID>", "signers": [{"roleName": "Customer", "name": "Priya Raman", "email": "priya@meridian.example.com"}]}`. Signing the envelope makes Connect deliver `envelope-completed` and the Flow completes. Set `LOG_LEVEL=debug` to see every delivery.

The [unit tests](../../test/envelope_outcome_workflow.test.ts) cover the correlation and input mapping. The [integration test](../../test/integration/envelope-outcome-trigger.integration.test.ts) runs this application against a real Dex Server with a local DocuSign stand-in and signed Connect deliveries. Live DocuSign Connect has not been verified.
