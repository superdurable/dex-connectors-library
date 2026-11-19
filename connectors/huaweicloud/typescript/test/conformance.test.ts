// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { HuaweiCloudClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/huaweicloud/scenarios.json', import.meta.url));
const connection = { provider: 'huaweicloud', name: 'huaweicloud' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeHuaweiCloud, ''));

async function invokeHuaweiCloud(scenario: Scenario, recorder: ExchangeRecorder) {
  const accountName = scenario.name.endsWith('defect') ? '' : 'example-account';
  const credentials: Array<[typeof connection, { accountName: SecretString; iamUsername: SecretString; iamPassword: SecretString }]> = [[connection, { accountName: new SecretString(accountName), iamUsername: new SecretString('example-user'), iamPassword: new SecretString('synthetic-test-password') }]];
  const client = new HuaweiCloudClient({ maxResponseBytes: scenario.name.endsWith('oversizedAuth') ? 16 : 1048576 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'huaweicloud-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'huaweicloud', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listAccessibleProjects') return client.listAccessibleProjects().invoke(call);
  throw new Error(`unknown HuaweiCloud operation ${scenario.operation}`);
}
