// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { FofaClient } from '../src/client.js';
import { SearchDomainAssetsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/fofa/scenarios.json', import.meta.url));
const connection = { provider: 'fofa', name: 'fofa' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeFofa, ''));

async function invokeFofa(scenario: Scenario, recorder: ExchangeRecorder) {
  const apiKey = scenario.name.endsWith('missing-credentials') ? '' : 'test-secret';
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = [[connection, { apiKey: new SecretString(apiKey) }]];
  const client = new FofaClient({ maxResponseBytes: scenario.name.endsWith('oversize') ? 8 : 1_048_576 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'fofa-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'fofa', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'searchDomainAssets') return client.searchDomainAssets().invoke(call, SearchDomainAssetsInputSchema.parse(scenario.input));
  throw new Error('unknown Fofa operation ' + scenario.operation);
}
