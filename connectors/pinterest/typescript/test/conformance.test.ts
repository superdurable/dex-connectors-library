// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { PinterestClient } from '../src/client.js';
import { ListPinsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/pinterest/scenarios.json', import.meta.url));
const connection = { provider: 'pinterest', name: 'pinterest' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokePinterest, ''));

async function invokePinterest(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials = {
    oauthClientId: 'synthetic-client-id', oauthClientSecret: new SecretString('synthetic-client-secret'),
    accessToken: new SecretString('test-pinterest-token'), refreshToken: new SecretString('synthetic-refresh-token'),
  };
  const client = new PinterestClient({ baseUrl: 'http://127.0.0.1:12345' }, new StaticCredentialProvider([[connection, credentials]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'pinterest-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'pinterest', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listPins') return client.listPins().invoke(call, ListPinsInputSchema.parse(scenario.input));
  throw new Error(`unknown Pinterest operation ${scenario.operation}`);
}
