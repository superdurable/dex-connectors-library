// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { BraintreeClient } from '../src/client.js';
import { GetTransactionInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/braintree/scenarios.json', import.meta.url));
const connection = { provider: 'braintree', name: 'braintree' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeBraintree, ''));

async function invokeBraintree(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { publicKey: string; privateKey: SecretString }]> = [[connection, { publicKey: 'sample-public-key', privateKey: new SecretString('sample-private-key') }]];
  const client = new BraintreeClient({ baseUrl: 'http://127.0.0.1:12345' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'braintree-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'braintree', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getTransaction') return client.getTransaction().invoke(call, GetTransactionInputSchema.parse(scenario.input));
  throw new Error(`unknown Braintree operation ${scenario.operation}`);
}
