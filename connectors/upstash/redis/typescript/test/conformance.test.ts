// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { UpstashRedisClient } from '../src/client.js';
import { GetKeyInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/upstash/redis/scenarios.json', import.meta.url));
const connection = { provider: 'upstash-redis', name: 'upstash-redis' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeUpstashRedis, ''));

async function invokeUpstashRedis(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { restToken: SecretString }]> = [[connection, { restToken: new SecretString('sample-redis-token') }]];
  const client = new UpstashRedisClient({ baseUrl: 'http://127.0.0.1:12345' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'redis-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'upstash-redis', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getKey') return client.getKey().invoke(call, GetKeyInputSchema.parse(scenario.input));
  throw new Error(`unknown Upstash Redis operation ${scenario.operation}`);
}
