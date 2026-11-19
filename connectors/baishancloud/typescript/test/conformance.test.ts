// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { BaishanCloudClient } from '../src/client.js';
import { DescribeDomainInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/baishancloud/scenarios.json', import.meta.url));
const connection = { provider: 'baishancloud', name: 'baishancloud' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeBaishanCloud, ''));

async function invokeBaishanCloud(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { token: SecretString }]> = [[connection, { token: new SecretString('secretToken') }]];
  const client = new BaishanCloudClient({ baseUrl: 'http://127.0.0.1:12345', maxResponseBytes: 262144 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'baishancloud-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'baishancloud', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'describeDomain') return client.describeDomain().invoke(call, DescribeDomainInputSchema.parse(scenario.input));
  throw new Error(`unknown BaishanCloud operation ${scenario.operation}`);
}
