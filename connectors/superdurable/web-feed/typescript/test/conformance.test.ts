// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import type { Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { WebFeedClient } from '../src/client.js';
import { DiscoverFeedsInputSchema, ReadFeedInputSchema, ReadPageInputSchema, ReadPageMetadataInputSchema, ReadSitemapInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type HeartbeatContext, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/superdurable/web-feed/scenarios.json', import.meta.url));
const connection = { provider: 'web-feed', name: 'web-feed-public' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeWebFeed));

async function invokeWebFeed(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const client = new WebFeedClient({}, {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
    now: () => context.fixedClock,
  });
  const call: Call = {
    context: { flowId: 'web-feed-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'web-feed', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  switch (scenario.operation) {
    case 'readFeed': return client.readFeed().invoke(call, ReadFeedInputSchema.parse(scenario.input));
    case 'discoverFeeds': return client.discoverFeeds().invoke(call, DiscoverFeedsInputSchema.parse(scenario.input));
    case 'readSitemap': return client.readSitemap().invoke(call, ReadSitemapInputSchema.parse(scenario.input));
    case 'readPageMetadata': return client.readPageMetadata().invoke(call, ReadPageMetadataInputSchema.parse(scenario.input));
    case 'readPage': return client.readPage().invoke(call, ReadPageInputSchema.parse(scenario.input));
  }
  throw new Error(`unknown web-feed operation ${scenario.operation}`);
}
