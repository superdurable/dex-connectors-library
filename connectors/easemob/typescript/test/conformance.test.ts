// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { EasemobClient } from '../src/client.js';
import { GetUserDetailInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/easemob/scenarios.json', import.meta.url));
const connection = { provider: 'easemob', name: 'easemob' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeEasemob, ''));

async function invokeEasemob(scenario: Scenario, recorder: ExchangeRecorder) {
  const secret = scenario.name.endsWith('missing-credentials') ? '' : 'client-secret';
  const credentials: Array<[typeof connection, { appKey: string; clientId: string; clientSecret: SecretString }]> = [[connection, { appKey: 'demo#chat', clientId: 'client-id', clientSecret: new SecretString(secret) }]];
  const client = new EasemobClient({ baseUrl: 'https://a1.easemob.com', maxResponseBytes: scenario.name.endsWith('oversize-user') ? 120 : 65536 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'easemob-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'easemob', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getUserDetail') return client.getUserDetail().invoke(call, GetUserDetailInputSchema.parse(scenario.input));
  throw new Error(`unknown Easemob operation ${scenario.operation}`);
}
