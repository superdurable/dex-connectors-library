// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { DedaoClient } from '../src/client.js';
import { GetNoteTaskStatusInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/dedao/scenarios.json', import.meta.url));
const connection = { provider: 'dedao', name: 'dedao' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeDedao, ''));

async function invokeDedao(scenario: Scenario, recorder: ExchangeRecorder) {
  const isMissing = scenario.name === 'getNoteTaskStatus-missing-key';
  const credentials = { apiKey: new SecretString(isMissing ? '' : 'gk_live_test-secret'), clientID: new SecretString(isMissing ? '' : 'cli_test-client') };
  const client = new DedaoClient(scenario.name === 'getNoteTaskStatus-oversize' ? { maxResponseBytes: 16 } : {},
    new StaticCredentialProvider([[connection, credentials]]), { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'dedao-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'dedao', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.getNoteTaskStatus().invoke(call, GetNoteTaskStatusInputSchema.parse(scenario.input));
}
