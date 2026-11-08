// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { RecurlyClient } from '../src/client.js';
import { GetAccountInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/recurly/scenarios.json', import.meta.url));
const connection = { provider: 'recurly', name: 'recurly' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeRecurly, '/api'));

async function invokeRecurly(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = [[connection, { apiKey: new SecretString('sample-recurly-api-key') }]];
  const client = new RecurlyClient({ baseUrl: 'http://127.0.0.1:12345/api' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'recurly-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'recurly', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getAccount') return client.getAccount().invoke(call, GetAccountInputSchema.parse(scenario.input));
  throw new Error(`unknown Recurly operation ${scenario.operation}`);
}
