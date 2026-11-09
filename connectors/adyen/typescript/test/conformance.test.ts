// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { AdyenClient } from '../src/client.js';
import { GetPaymentLinkInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/adyen/scenarios.json', import.meta.url));
const connection = { provider: 'adyen', name: 'adyen' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeAdyen, '/v72'));

async function invokeAdyen(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = [[connection, { apiKey: new SecretString('sample-adyen-api-key') }]];
  const client = new AdyenClient({ baseUrl: 'http://127.0.0.1:12345/v72' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'adyen-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'adyen', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getPaymentLink') return client.getPaymentLink().invoke(call, GetPaymentLinkInputSchema.parse(scenario.input));
  throw new Error(`unknown Adyen operation ${scenario.operation}`);
}
