// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { NiutransClient } from '../src/client.js';
import { TranslateTextInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/niutrans/scenarios.json', import.meta.url));
const connection = { provider: 'niutrans', name: 'niutrans' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeNiutrans, ''));

async function invokeNiutrans(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials = new StaticCredentialProvider([[connection, { apiKey: new SecretString("test-translation-api-key") }]]);
  const client = new NiutransClient({}, credentials, {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'niutrans-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'niutrans', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.translateText().invoke(call, TranslateTextInputSchema.parse(scenario.input));
}
