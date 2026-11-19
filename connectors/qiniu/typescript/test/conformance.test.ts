// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { QiniuClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/qiniu/scenarios.json', import.meta.url));
const connection = { provider: 'qiniu', name: 'qiniu' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeQiniu, ''));

async function invokeQiniu(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { accessKey: SecretString; secretKey: SecretString }]> = [[connection, { accessKey: new SecretString(scenario.name.endsWith('defect') ? '' : 'test-access-key'), secretKey: new SecretString('test-secret-key') }]];
  const client = new QiniuClient({ baseUrl: 'http://127.0.0.1:12345', maxResponseBytes: scenario.name.endsWith('oversized') ? 16 : 262144 }, new StaticCredentialProvider(credentials), {
    now: () => new Date('2026-11-14T00:30:05Z'),
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'qiniu-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'qiniu', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listBuckets') return client.listBuckets().invoke(call);
  throw new Error(`unknown Qiniu operation ${scenario.operation}`);
}
