// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { DropboxSignClient } from '../src/client.js';
import { GetSignatureRequestInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/dropbox/sign/scenarios.json', import.meta.url));
const connection = { provider: 'dropbox-sign', name: 'dropbox-sign' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeDropboxSign, '/v3'));

async function invokeDropboxSign(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = [[connection, { apiKey: new SecretString('sample-dropbox-sign-api-key') }]];
  const client = new DropboxSignClient({ baseUrl: 'http://127.0.0.1:12345/v3' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'dropbox-sign-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'dropbox-sign', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getSignatureRequest') return client.getSignatureRequest().invoke(call, GetSignatureRequestInputSchema.parse(scenario.input));
  throw new Error(`unknown DropboxSign operation ${scenario.operation}`);
}
