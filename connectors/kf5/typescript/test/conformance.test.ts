// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { KF5Client } from '../src/client.js';
import { GetTicketInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/kf5/scenarios.json', import.meta.url));
const connection = { provider: 'kf5', name: 'kf5' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeKF5, ''));

async function invokeKF5(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { apiToken: SecretString; email: SecretString; subdomain: string }]> = [[connection, { apiToken: new SecretString('private-token'), email: new SecretString('agent@example.com'), subdomain: 'test-tenant' }]];
  const client = new KF5Client({ maxResponseBytes: 1_048_576 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'kf5-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'kf5', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getTicket') return client.getTicket().invoke(call, GetTicketInputSchema.parse(scenario.input));
  throw new Error('unknown KF5 operation ' + scenario.operation);
}
