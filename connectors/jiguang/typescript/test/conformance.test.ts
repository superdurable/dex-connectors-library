// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { JiguangClient } from '../src/client.js';
import { ListApplicationTagsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/jiguang/scenarios.json', import.meta.url));
const connection = { provider: 'jiguang', name: 'jiguang' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeJiguang, ''));

async function invokeJiguang(scenario: Scenario, recorder: ExchangeRecorder) {
  const appKey = scenario.name.endsWith('invalid-credentials') ? 'bad key' : '7d431e42dfa6a6d693ac2d04';
  const credentials: Array<[typeof connection, { appKey: string; masterSecret: SecretString }]> = [[connection, { appKey, masterSecret: new SecretString('5e987ac6d2e04d95a9d8f0d1') }]];
  const client = new JiguangClient({ baseUrl: 'http://127.0.0.1:12345', maxResponseBytes: scenario.name.endsWith('oversize') ? 8 : 65536 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'jiguang-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'jiguang', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listApplicationTags') return client.listApplicationTags().invoke(call, ListApplicationTagsInputSchema.parse(scenario.input));
  throw new Error(`unknown Jiguang operation ${scenario.operation}`);
}
