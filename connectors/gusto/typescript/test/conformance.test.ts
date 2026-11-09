// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { GustoClient } from '../src/client.js';
import { GetEmployeeInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/gusto/scenarios.json', import.meta.url));
const connection = { provider: 'gusto', name: 'gusto' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeGusto, ''));

async function invokeGusto(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials = {
    oauthClientId: 'synthetic-client-id', oauthClientSecret: new SecretString('synthetic-client-secret'),
    accessToken: new SecretString('test-gusto-token'), refreshToken: new SecretString('synthetic-refresh-token'),
  };
  const client = new GustoClient({ redirectUri: 'https://dex.example/callback', baseUrl: 'http://127.0.0.1:12345' }, new StaticCredentialProvider([[connection, credentials]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'gusto-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'gusto', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getEmployee') return client.getEmployee().invoke(call, GetEmployeeInputSchema.parse(scenario.input));
  throw new Error(`unknown Gusto operation ${scenario.operation}`);
}
