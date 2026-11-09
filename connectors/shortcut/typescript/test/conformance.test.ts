// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { ShortcutClient } from '../src/client.js';
import { GetStoryInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/shortcut/scenarios.json', import.meta.url));
const connection = { provider: 'shortcut', name: 'shortcut' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeShortcut, '/api/v3'));

async function invokeShortcut(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { apiToken: SecretString }]> = [[connection, { apiToken: new SecretString('sample-shortcut-token') }]];
  const client = new ShortcutClient({ baseUrl: 'http://127.0.0.1:12345/api/v3' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'shortcut-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'shortcut', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getStory') return client.getStory().invoke(call, GetStoryInputSchema.parse(scenario.input));
  throw new Error(`unknown Shortcut operation ${scenario.operation}`);
}
