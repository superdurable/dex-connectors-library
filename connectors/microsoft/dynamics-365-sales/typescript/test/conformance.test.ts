// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { MicrosoftDynamics365SalesClient } from '../src/client.js';
import { FindContactInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/microsoft/dynamics-365-sales/scenarios.json', import.meta.url));
const connection = { provider: 'microsoft-dynamics-365-sales', name: 'microsoft-dynamics-365-sales' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeMicrosoftDynamics365Sales, ''));

async function invokeMicrosoftDynamics365Sales(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { clientSecret: SecretString }]> = [[connection, { clientSecret: new SecretString('test-client-secret') }]];
  const client = new MicrosoftDynamics365SalesClient({ environmentUrl: 'http://127.0.0.1:12345', tenantId: '11111111-1111-4111-8111-111111111111', clientId: '22222222-2222-4222-8222-222222222222' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
    tokenEndpoint: 'http://127.0.0.1:12345/token',
  });
  const call: Call = {
    context: { flowId: 'dynamics365sales-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'microsoft-dynamics-365-sales', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'findContact') return client.findContact().invoke(call, FindContactInputSchema.parse(scenario.input));
  throw new Error(`unknown Microsoft Dynamics 365 Sales operation ${scenario.operation}`);
}
