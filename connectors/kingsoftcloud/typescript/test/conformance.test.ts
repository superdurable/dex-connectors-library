// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { KingsoftCloudClient } from '../src/client.js';
import { ListUsersInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/kingsoftcloud/scenarios.json', import.meta.url));
const connection = { provider: 'kingsoftcloud', name: 'kingsoftcloud' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeKingsoftCloud, ''));

async function invokeKingsoftCloud(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { accessKeyId: SecretString; secretAccessKey: SecretString }]> = [[connection, { accessKeyId: new SecretString('test-access-key-id'), secretAccessKey: new SecretString('test-secret-access-key') }]];
  const client = new KingsoftCloudClient({ baseUrl: 'http://127.0.0.1:12345', maxResponseBytes: scenario.name.endsWith('-oversized') ? 16 : undefined }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
    now: () => new Date('2021-08-12T02:47:36Z'),
  });
  const call: Call = {
    context: { flowId: 'kingsoftcloud-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'kingsoftcloud', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listUsers') return client.listUsers().invoke(call, ListUsersInputSchema.parse(scenario.input));
  throw new Error(`unknown Kingsoft Cloud operation ${scenario.operation}`);
}
