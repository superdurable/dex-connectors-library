// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { HonorClient } from '../src/client.js';
import { LookupAppIDInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/honor/scenarios.json', import.meta.url));
const connection = { provider: 'honor', name: 'honor' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeHonor, ''));

async function invokeHonor(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { clientID: SecretString; clientSecret: SecretString }]> = [[connection, { clientID: new SecretString('test-client'), clientSecret: new SecretString('test-secret') }]];
  const client = new HonorClient({ maxResponseBytes: scenario.name.endsWith('oversize') ? 8 : 1_048_576 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'honor-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'honor', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'lookupAppID') return client.lookupAppID().invoke(call, LookupAppIDInputSchema.parse(scenario.input));
  throw new Error('unknown Honor operation ' + scenario.operation);
}
