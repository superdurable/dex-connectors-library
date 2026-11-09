// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { RabbitMQClient } from '../src/client.js';
import { GetQueueMetricsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/rabbitmq/scenarios.json', import.meta.url));
const connection = { provider: 'rabbitmq', name: 'rabbitmq' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeRabbitMQ, ''));

async function invokeRabbitMQ(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { username: string; password: SecretString }]> = [[connection, { username: 'observer', password: new SecretString('sample-password') }]];
  const client = new RabbitMQClient({ managementUrl: 'http://127.0.0.1:12345' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'rabbitmq-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'rabbitmq', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getQueueMetrics') return client.getQueueMetrics().invoke(call, GetQueueMetricsInputSchema.parse(scenario.input));
  throw new Error(`unknown RabbitMQ operation ${scenario.operation}`);
}
