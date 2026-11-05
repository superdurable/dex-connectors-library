// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ExchangeRecorder, loadScenarios, replay, scenarioCall, withScenarioHTTPServer, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { VercelClient } from '../src/client.js';
import { GetDeploymentInputSchema, ListDeploymentsInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/vercel/scenarios.json', import.meta.url));
const connection = { provider: 'vercel', name: 'vercel' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeVercel));

async function invokeVercel(scenario: Scenario, recorder: ExchangeRecorder) {
  return withScenarioHTTPServer(recorder, async baseUrl => {
    const client = new VercelClient({ baseUrl }, new StaticCredentialProvider([[connection, { accessToken: new SecretString(scenario.name === 'listDeployments-defect' ? '' : 'test-accesstoken') }]]));
    const call = scenarioCall(scenario, connection, 'vercel', { flowId: 'vercel-conformance', cancellationSignal: new AbortController().signal }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
    if (scenario.operation === 'listDeployments') return client.listDeployments().invoke(call, ListDeploymentsInputSchema.parse(scenario.input));
    if (scenario.operation === 'getDeployment') return client.getDeployment().invoke(call, GetDeploymentInputSchema.parse(scenario.input));
    throw new TypeError(`unknown operation ${scenario.operation}`);
  });
}
