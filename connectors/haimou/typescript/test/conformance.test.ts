// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { HaimouClient } from '../src/client.js';
import { LookupIPLocationInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/haimou/scenarios.json', import.meta.url));
const connection = { provider: 'haimou', name: 'haimou' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeHaimou, ''));

async function invokeHaimou(scenario: Scenario, recorder: ExchangeRecorder) {
  const apiKey = scenario.name.endsWith('missing-credentials') ? '' : 'private-test-key';
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = [[connection, { apiKey: new SecretString(apiKey) }]];
  const client = new HaimouClient({ maxResponseBytes: scenario.name.endsWith('oversize') ? 8 : 1_048_576 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'haimou-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'haimou', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'lookupIPLocation') return client.lookupIPLocation().invoke(call, LookupIPLocationInputSchema.parse(scenario.input));
  throw new Error('unknown Haimou operation ' + scenario.operation);
}
