// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { WooCommerceClient } from '../src/client.js';
import { GetOrderInputSchema, ListOrdersInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/woocommerce/scenarios.json', import.meta.url));
const connection = { provider: 'woocommerce', name: 'woocommerce' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeWooCommerce, '/shop'));

async function invokeWooCommerce(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new WooCommerceClient({ storeUrl: 'http://localhost/shop' }, {
    resolve: async () => ({ consumerKey: new SecretString('ck_test'), consumerSecret: new SecretString('cs_test') }),
  }, { send: sender });
  const call = scenarioCall(scenario, connection, 'woocommerce', { flowId: 'woocommerce-conformance', cancellationSignal: new AbortController().signal }, {
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  }) as Call;
  if (scenario.operation === 'listOrders') return client.listOrders().invoke(call, ListOrdersInputSchema.parse(scenario.input));
  if (scenario.operation === 'getOrder') return client.getOrder().invoke(call, GetOrderInputSchema.parse(scenario.input));
  throw new Error(`unknown WooCommerce operation ${scenario.operation}`);
}
