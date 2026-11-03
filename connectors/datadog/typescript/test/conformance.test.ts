// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ExchangeRecorder, loadScenarios, replay, withScenarioHTTPServer, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { DatadogClient } from '../src/client.js';
import { ListMonitorsInputSchema, QueryMetricsInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/datadog/scenarios.json', import.meta.url));
const connection = { provider: 'datadog', name: 'datadog' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeDatadog));

async function invokeDatadog(scenario: Scenario, recorder: ExchangeRecorder) {
  return withScenarioHTTPServer(recorder, async baseUrl => {
    const client = new DatadogClient({ baseUrl }, new StaticCredentialProvider([[connection, {
      apiKey: new SecretString('test-apikey'), applicationKey: new SecretString('test-applicationkey'),
    }]]));
    const call: Call = {
      context: { flowId: 'datadog-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
      id: scenario.callId, connection, operation: { connectorId: 'datadog', operationId: scenario.operation },
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    if (scenario.operation === 'queryMetrics') return client.queryMetrics().invoke(call, QueryMetricsInputSchema.parse(scenario.input));
    if (scenario.operation === 'listMonitors') return client.listMonitors().invoke(call, ListMonitorsInputSchema.parse(scenario.input));
    throw new Error(`unknown Datadog operation ${scenario.operation}`);
  });
}
