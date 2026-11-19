// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { UPYUNClient } from '../src/client.js';
import { GetServiceUsageInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/upyun/scenarios.json', import.meta.url));
const connection = { provider: 'upyun', name: 'upyun' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeUPYUN, ''));

async function invokeUPYUN(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { operator: string; password: SecretString }]> = [[connection, { operator: 'upyun', password: new SecretString('password') }]];
  if (scenario.name.endsWith('-defect')) credentials.length = 0;
  const client = new UPYUNClient({ baseUrl: 'http://127.0.0.1:12345', bucket: 'bucket', maxResponseBytes: scenario.name.endsWith('-oversized') ? 3 : undefined }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
    now: () => new Date('2025-01-02T03:04:05Z'),
  });
  const call: Call = {
    context: { flowId: 'upyun-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'upyun', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getServiceUsage') return client.getServiceUsage().invoke(call, GetServiceUsageInputSchema.parse(scenario.input));
  throw new Error(`unknown UPYUN operation ${scenario.operation}`);
}
