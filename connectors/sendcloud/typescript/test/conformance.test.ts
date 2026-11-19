// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { SendCloudClient } from '../src/client.js';
import { GetAccountBalanceInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/sendcloud/scenarios.json', import.meta.url));
const connection = { provider: 'sendcloud', name: 'sendcloud' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeSendCloud, ''));

async function invokeSendCloud(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { apiUser: SecretString; apiKey: SecretString }]> = [[connection, { apiUser: new SecretString('test-api-user'), apiKey: new SecretString('test-api-key') }]];
  if (scenario.name.endsWith('-defect')) credentials.length = 0;
  const client = new SendCloudClient({ baseUrl: 'http://127.0.0.1:12345', maxResponseBytes: scenario.name.endsWith('-oversized') ? 3 : undefined }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'sendcloud-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'sendcloud', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getAccountBalance') return client.getAccountBalance().invoke(call, GetAccountBalanceInputSchema.parse(scenario.input));
  throw new Error(`unknown SendCloud operation ${scenario.operation}`);
}
