// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { RazorpayClient } from '../src/client.js';
import { GetPaymentInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/razorpay/scenarios.json', import.meta.url));
const connection = { provider: 'razorpay', name: 'razorpay' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeRazorpay, '/api'));

async function invokeRazorpay(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { keyId: SecretString; keySecret: SecretString }]> = [[connection, { keyId: new SecretString('rzp_test_1234567890'), keySecret: new SecretString('sample-razorpay-secret') }]];
  const client = new RazorpayClient({ baseUrl: 'http://127.0.0.1:12345/api' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'razorpay-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'razorpay', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getPayment') return client.getPayment().invoke(call, GetPaymentInputSchema.parse(scenario.input));
  throw new Error(`unknown Razorpay operation ${scenario.operation}`);
}
