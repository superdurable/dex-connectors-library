// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { VolcengineClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/volcengine/scenarios.json', import.meta.url));
const connection = { provider: 'volcengine', name: 'volcengine' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeVolcengine, ''));

async function invokeVolcengine(scenario: Scenario, recorder: ExchangeRecorder) {
  const accessKeyId = scenario.name.endsWith('defect') ? '' : 'AKLTtest-access-key';
  const credentials: Array<[typeof connection, { accessKeyId: SecretString; secretAccessKey: SecretString }]> = [[connection, { accessKeyId: new SecretString(accessKeyId), secretAccessKey: new SecretString('test-secret-access-key') }]];
  const client = new VolcengineClient({ baseUrl: 'http://127.0.0.1:12345', maxResponseBytes: scenario.name.endsWith('oversized') ? 8 : 1048576 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)), now: () => new Date('2025-03-29T18:09:37Z'),
  });
  const call: Call = {
    context: { flowId: 'volcengine-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'volcengine', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'describeRegions') return client.describeRegions().invoke(call, scenario.input as { maxResults?: number; nextToken?: string });
  throw new Error(`unknown Volcengine operation ${scenario.operation}`);
}
