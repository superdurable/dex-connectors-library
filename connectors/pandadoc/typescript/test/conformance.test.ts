// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { PandaDocClient } from '../src/client.js';
import { GetDocumentInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/pandadoc/scenarios.json', import.meta.url));
const connection = { provider: 'pandadoc', name: 'pandadoc' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokePandaDoc, '/public/v1'));

async function invokePandaDoc(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = [[connection, { apiKey: new SecretString('sample-pandadoc-api-key') }]];
  const client = new PandaDocClient({ baseUrl: 'http://127.0.0.1:12345/public/v1' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'pandadoc-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'pandadoc', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getDocument') return client.getDocument().invoke(call, GetDocumentInputSchema.parse(scenario.input));
  throw new Error(`unknown PandaDoc operation ${scenario.operation}`);
}
