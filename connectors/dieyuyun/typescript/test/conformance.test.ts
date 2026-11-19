// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { DieyuyunClient } from '../src/client.js';
import { GetAccountBalanceInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/dieyuyun/scenarios.json', import.meta.url));
const connection = { provider: 'dieyuyun', name: 'dieyuyun' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeDieyuyun, ''));

async function invokeDieyuyun(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials = { apiKey: new SecretString(scenario.name === 'getAccountBalance-missing-key' ? '' : 'private-test-key') };
  const client = new DieyuyunClient(scenario.name === 'getAccountBalance-oversize' ? { maxResponseBytes: 8 } : {},
    new StaticCredentialProvider([[connection, credentials]]), { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'dieyuyun-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'dieyuyun', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.getAccountBalance().invoke(call, GetAccountBalanceInputSchema.parse(scenario.input));
}
