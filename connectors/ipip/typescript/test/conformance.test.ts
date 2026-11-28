// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { IPIPClient } from '../src/client.js';
import { LookupIPLocationInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/ipip/scenarios.json', import.meta.url));
const connection = { provider: 'ipip', name: 'ipip' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeIPIP));

async function invokeIPIP(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'lookupIPLocation') throw new Error(`unknown IPIP operation ${scenario.operation}`);
  const client = new IPIPClient(scenario.name.endsWith("-oversized") ? { maxResponseBytes: 16 } : {}, new StaticCredentialProvider([[connection, { token: new SecretString(scenario.name === 'lookupIPLocation-missingCredential' ? '' : 'test-secret') }]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'ipip-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'ipip', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.lookupIPLocation().invoke(call, LookupIPLocationInputSchema.parse(scenario.input));
}
