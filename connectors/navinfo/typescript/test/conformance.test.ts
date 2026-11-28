// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { NavInfoClient } from '../src/client.js';
import { LookupIPLocationInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/navinfo/scenarios.json', import.meta.url));
const connection = { provider: 'navinfo', name: 'navinfo' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeNavInfo, ''));

async function invokeNavInfo(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials = new StaticCredentialProvider([[connection, { ak: new SecretString(scenario.name === 'lookupIPLocation-missingCredential' ? '' : 'test-secret') }]]);
  const client = new NavInfoClient(scenario.name.endsWith("-oversized") ? { maxResponseBytes: 16 } : {}, credentials, {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'navinfo-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'navinfo', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.lookupIPLocation().invoke(call, LookupIPLocationInputSchema.parse(scenario.input));
}
