// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { CaoliaoClient } from '../src/client.js';
import { ListQRCodesInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/caoliao/scenarios.json', import.meta.url));
const connection = { provider: 'caoliao', name: 'caoliao' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeCaoliao, ''));

async function invokeCaoliao(scenario: Scenario, recorder: ExchangeRecorder) {
  const apiKey = scenario.name.endsWith('defect') ? '' : 'caoliao-test-secret';
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = [[connection, { apiKey: new SecretString(apiKey) }]];
  const client = new CaoliaoClient({ baseUrl: 'http://127.0.0.1:12345', maxResponseBytes: scenario.name.endsWith('oversized') ? 8 : 1_048_576 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'caoliao-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'caoliao', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listQRCodes') return client.listQRCodes().invoke(call, ListQRCodesInputSchema.parse(scenario.input));
  throw new Error('unknown Caoliao operation ' + scenario.operation);
}
