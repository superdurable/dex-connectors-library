// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { PayPalClient } from '../src/client.js';
import { GetOrderInputSchema, ListTransactionsInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/paypal/scenarios.json', import.meta.url));
const connection = { provider: 'paypal', name: 'paypal-test' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokePayPal));

async function invokePayPal(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new PayPalClient({ baseUrl: 'http://127.0.0.1:12345' }, {
    resolve: async () => ({ clientId: 'client-test', clientSecret: new SecretString('secret-test') }),
  }, { send: sender });
  const call = scenarioCall(scenario, connection, 'paypal', { flowId: 'paypal-flow', cancellationSignal: new AbortController().signal }) as Call;
  if (scenario.operation === 'getOrder') return client.getOrder().invoke(call, GetOrderInputSchema.parse(scenario.input));
  if (scenario.operation === 'listTransactions') return client.listTransactions().invoke(call, ListTransactionsInputSchema.parse(scenario.input));
  throw new Error(`unknown PayPal operation ${scenario.operation}`);
}
