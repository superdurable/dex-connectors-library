// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { Client } from '../src/client.js';
import { GetPaymentInputSchema, ListPaymentsInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/square/scenarios.json', import.meta.url));
const connection = { provider: 'square', name: 'square' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeSquare));

async function invokeSquare(scenario: Scenario, recorder: ExchangeRecorder) {
  const send = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new Client({ baseUrl: 'http://127.0.0.1:12345/v2' }, {
    resolve: async () => ({ accessToken: new SecretString(scenario.name === 'listPayments-defect' ? '' : 'test-token') }),
  }, { send });
  const call = scenarioCall(scenario, connection, 'square', {
    flowId: 'square-conformance', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'listPayments') return client.listPayments().invoke(call, ListPaymentsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getPayment') return client.getPayment().invoke(call, GetPaymentInputSchema.parse(scenario.input));
  throw new Error(`unknown Square operation ${scenario.operation}`);
}
