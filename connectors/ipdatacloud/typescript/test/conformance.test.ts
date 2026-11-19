// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { IPDataCloudClient } from '../src/client.js';
import { LookupIPLocationInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/ipdatacloud/scenarios.json', import.meta.url));
const connection = { provider: 'ipdatacloud', name: 'ipdatacloud' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeIPDataCloud));

async function invokeIPDataCloud(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'lookupIPLocation') throw new Error(`unknown IPDataCloud operation ${scenario.operation}`);
  const client = new IPDataCloudClient({}, new StaticCredentialProvider([[connection, { apiKey: new SecretString('private-test-key') }]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'ipdatacloud-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'ipdatacloud', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.lookupIPLocation().invoke(call, LookupIPLocationInputSchema.parse(scenario.input));
}
