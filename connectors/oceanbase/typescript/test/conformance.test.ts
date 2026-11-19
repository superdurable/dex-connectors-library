// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { OceanbaseClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, SyntheticRandomSource, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/oceanbase/scenarios.json', import.meta.url));
const connection = { provider: 'oceanbase', name: 'oceanbase' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeOceanbase, ''));

async function invokeOceanbase(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listInstances') throw new Error(`unknown OceanBase operation ${scenario.operation}`);
  const credentials = new StaticCredentialProvider([[connection, {
    accessKeyId: new SecretString('REDACTED_OCEANBASE_ACCESS_KEY'),
    accessKeySecret: new SecretString('REDACTED_OCEANBASE_SECRET'),
    projectId: new SecretString('REDACTED_PROJECT_ID'),
  }]]);
  const random = new SyntheticRandomSource(scenario.randomSeed ?? 'SYNTHETIC_CONFORMANCE_OCEANBASE_fallback');
  const client = new OceanbaseClient({
    site: scenario.name.includes('international') ? 'international' : 'domestic',
    maxResponseBytes: scenario.name.includes('oversized') ? 16 : 1_048_576,
  }, credentials, {
    nonce: () => random.read(16),
    now: () => new Date(scenario.fixedClock ?? '2026-11-12T10:00:00Z'),
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'oceanbase-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId,
    connection,
    operation: { connectorId: 'oceanbase', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listInstances().invoke(call, scenario.input as Parameters<ReturnType<OceanbaseClient['listInstances']>['invoke']>[1]);
}
