// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type HeartbeatContext, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { DocuSignClient } from '../src/client.js';
import { CreateEnvelopeFromTemplateInputSchema, DownloadCombinedDocumentInputSchema, GetEnvelopeInputSchema, ListEnvelopeRecipientsInputSchema, VoidEnvelopeInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/docusign/scenarios.json', import.meta.url));
const connection = { provider: 'docusign', name: 'esignature' };
const credentials = { authMethodId: 'docusign-oauth', oauthClientId: 'integration-key', oauthClientSecret: new SecretString('SENTINEL-DOCUSIGN-CLIENT-SECRET'), accessToken: new SecretString('SENTINEL-DOCUSIGN-ACCESS-TOKEN'), refreshToken: new SecretString('SENTINEL-DOCUSIGN-REFRESH-TOKEN'), connectHmacKey: new SecretString('SENTINEL-DOCUSIGN-CONNECT-HMAC-KEY') };
const fixedNow = new Date('2026-10-04T12:00:00Z');
const clients = new WeakMap<ExchangeRecorder, DocuSignClient>();

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeDocuSign));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key, recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeDocuSign(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const send = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  let client = clients.get(recorder);
  if (!client) {
    const provider = new StaticCredentialProvider([[connection, credentials]]);
    client = new DocuSignClient({ ...(scenario.name === 'download-combined-document-too-large' ? { maxDocumentBytes: 10 } : {}) }, provider, { send, now: () => fixedNow });
    clients.set(recorder, client);
  }
  Object.assign(context, { flowId: 'docusign-flow', firstAttemptAt: fixedNow, cancellationSignal: new AbortController().signal });
  const call = scenarioCall(scenario, connection, 'docusign', context, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'createEnvelopeFromTemplate') return productionMutation(client.createEnvelopeFromTemplate(), call, CreateEnvelopeFromTemplateInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'getEnvelope') return client.getEnvelope().invoke(call, GetEnvelopeInputSchema.parse(scenario.input));
  if (scenario.operation === 'listEnvelopeRecipients') return client.listEnvelopeRecipients().invoke(call, ListEnvelopeRecipientsInputSchema.parse(scenario.input));
  if (scenario.operation === 'voidEnvelope') return productionMutation(client.voidEnvelope(), call, VoidEnvelopeInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'downloadCombinedDocument') return client.downloadCombinedDocument().invoke(call, DownloadCombinedDocumentInputSchema.parse(scenario.input));
  throw new Error(`unknown DocuSign operation ${scenario.operation}`);
}
