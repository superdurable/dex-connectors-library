// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { DataEyesClient } from '../src/client.js';
import { ReadWebPageInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/dataeyes/scenarios.json', import.meta.url));
const connection = { provider: 'dataeyes', name: 'dataeyes' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeDataEyes, ''));

async function invokeDataEyes(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials = { apiKey: new SecretString(scenario.name === 'readWebPage-missing-key' ? '' : 'test-secret') };
  const client = new DataEyesClient(scenario.name === 'readWebPage-oversize' ? { maxResponseBytes: 16 } : {},
    new StaticCredentialProvider([[connection, credentials]]), { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'dataeyes-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'dataeyes', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.readWebPage().invoke(call, ReadWebPageInputSchema.parse(scenario.input));
}
