// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { NewayzClient } from '../src/client.js';
import { GetCurrentWeatherInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/newayz/scenarios.json', import.meta.url));
const connection = { provider: 'newayz', name: 'newayz' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeNewayz, ''));

async function invokeNewayz(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials = new StaticCredentialProvider([[connection, { accessKey: new SecretString("test-newayz-access-key") }]]);
  const client = new NewayzClient({}, credentials, {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'newayz-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'newayz', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.getCurrentWeather().invoke(call, GetCurrentWeatherInputSchema.parse(scenario.input));
}
