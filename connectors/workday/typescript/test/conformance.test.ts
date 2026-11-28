// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { WorkdayClient } from '../src/client.js';
import { GetWorkerInputSchema, ListWorkersInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/workday/scenarios.json', import.meta.url));
const connection = { provider: 'workday', name: 'workday' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeWorkday, ''));

async function invokeWorkday(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials = {
    oauthClientId: 'synthetic-client-id', oauthClientSecret: new SecretString('synthetic-client-secret'),
    accessToken: new SecretString('test-workday-token'), refreshToken: new SecretString('synthetic-refresh-token'),
  };
  const client = new WorkdayClient({
    baseUrl: 'http://127.0.0.1:12345/api/common/v1/demo',
    authorizationEndpoint: 'http://127.0.0.1:12345/authorize',
    tokenEndpoint: 'http://127.0.0.1:12345/token',
    scopes: ['Staffing'],
    maxResponseBytes: scenario.name.endsWith('-smallLimit') ? 16 : 1048576,
  }, new StaticCredentialProvider([[connection, credentials]]), {
    send: async request => {
      const response = await answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
      if (response.body.length > request.maxResponseBytes!) throw new BodyTooLargeError(response.status, response.headers);
      return response;
    },
  });
  const call: Call = {
    context: { flowId: 'workday-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'workday', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getWorker') return client.getWorker().invoke(call, GetWorkerInputSchema.parse(scenario.input));
  if (scenario.operation === 'listWorkers') return client.listWorkers().invoke(call, ListWorkersInputSchema.parse(scenario.input));
  throw new Error(`unknown Workday operation ${scenario.operation}`);
}
