// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { BufferClient } from '../src/client.js';
import { GetChannelInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/buffer/scenarios.json', import.meta.url));
const connection = { provider: 'buffer', name: 'buffer' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeBuffer, ''));

async function invokeBuffer(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = [[connection, { apiKey: new SecretString('sample-api-key') }]];
  const client = new BufferClient({ baseUrl: 'http://127.0.0.1:12345' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'buffer-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'buffer', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getChannel') return client.getChannel().invoke(call, GetChannelInputSchema.parse(scenario.input));
  throw new Error(`unknown Buffer operation ${scenario.operation}`);
}
