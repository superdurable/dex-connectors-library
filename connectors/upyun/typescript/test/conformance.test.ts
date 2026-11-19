// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { UpyunClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/upyun/scenarios.json', import.meta.url));
const connection = { provider: 'upyun', name: 'upyun' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeUpyun, ''));

async function invokeUpyun(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { operator: string; password: SecretString }]> = [[connection, { operator: scenario.name.endsWith('defect') ? '' : 'upyun', password: new SecretString('password') }]];
  const client = new UpyunClient({ bucket: 'bucket', maxResponseBytes: scenario.name.endsWith('oversized') ? 4 : 1024 }, new StaticCredentialProvider(credentials), {
    now: () => new Date('2025-01-02T03:04:05Z'),
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'upyun-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'upyun', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getServiceUsage') return client.getServiceUsage().invoke(call);
  throw new Error(`unknown Upyun operation ${scenario.operation}`);
}
