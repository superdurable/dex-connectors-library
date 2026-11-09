// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { CustomerioClient } from '../src/client.js';
import { GetCustomersByEmailInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/customerio/scenarios.json', import.meta.url));
const connection = { provider: 'customerio', name: 'customerio' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeCustomerio, ''));

async function invokeCustomerio(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { privateKey: SecretString }]> = [[connection, { privateKey: new SecretString('sample-customerio-private-key') }]];
  const client = new CustomerioClient({ baseUrl: 'http://127.0.0.1:12345' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'customerio-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'customerio', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getCustomersByEmail') return client.getCustomersByEmail().invoke(call, GetCustomersByEmailInputSchema.parse(scenario.input));
  throw new Error(`unknown Customerio operation ${scenario.operation}`);
}
