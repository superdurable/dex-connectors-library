// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { OdooClient } from '../src/client.js';
import { SearchPartnersInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/odoo/scenarios.json', import.meta.url));
const connection = { provider: 'odoo', name: 'odoo' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeOdoo, ''));

async function invokeOdoo(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = [[connection, { apiKey: new SecretString('sample-odoo-api-key') }]];
  const client = new OdooClient({ baseUrl: 'http://127.0.0.1:12345', database: 'mycompany' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'odoo-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'odoo', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'searchPartners') return client.searchPartners().invoke(call, SearchPartnersInputSchema.parse(scenario.input));
  throw new Error(`unknown Odoo operation ${scenario.operation}`);
}
