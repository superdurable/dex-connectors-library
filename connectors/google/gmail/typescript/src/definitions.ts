// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { MutationDefinition, QueryDefinition } from '@superdurable/connectors-sdk';
import { ConnectorID } from './models.js';

/** Stable getMessage routes. */
export const GetMessageBranch = { Read: 'read', NotFound: 'notFound', ProviderRejected: 'providerRejected', InvalidResponse: 'invalidResponse', Defect: 'defect' } as const;
/** Stable sendMessage routes. */
export const SendMessageBranch = { Sent: 'sent', ProviderRejected: 'providerRejected', Defect: 'defect' } as const;
/** Stable replyToMessage routes. */
export const ReplyToMessageBranch = { Sent: 'sent', ProviderRejected: 'providerRejected', InvalidResponse: 'invalidResponse', Defect: 'defect' } as const;

/** Manifest-derived getMessage definition. */
export const GetMessageDefinition: QueryDefinition = {
  operation: { connectorId: ConnectorID, operationId: 'getMessage' },
  branches: [
    { id: 'read', description: 'The Gmail message was read.' },
    { id: 'notFound', description: 'The Gmail message does not exist.', optional: true },
    { id: 'providerRejected', description: 'Gmail conclusively rejected the message query.', optional: true },
    { id: 'invalidResponse', description: 'Gmail returned an invalid or oversized message response.', optional: true },
    { id: 'defect', description: 'Local input, connection configuration, or connector definition is invalid.', optional: true },
  ],
  stepDefaults: { executeMethodTimeoutMs: 30_000, executeDurability: 'async' },
};

/** Manifest-derived sendMessage definition. */
export const SendMessageDefinition: MutationDefinition = {
  operation: { connectorId: ConnectorID, operationId: 'sendMessage' },
  branches: [
    { id: 'sent', description: 'Gmail accepted the message; its message and thread IDs can be empty when an unusable accepted response has no Sent Mail match.' },
    { id: 'providerRejected', description: 'Gmail conclusively rejected the message.', optional: true },
    { id: 'defect', description: 'Local input, connection configuration, or connector definition is invalid.', optional: true },
  ],
  stepDefaults: { executeMethodTimeoutMs: 30_000, executeDurability: 'async', executeRetry: {
    initialIntervalMs: 1000, backoffCoefficient: 2, maximumIntervalMs: 30_000, maximumAttempts: 5, totalDurationMs: 120_000,
  } },
};

/** Manifest-derived replyToMessage definition. */
export const ReplyToMessageDefinition: MutationDefinition = {
  operation: { connectorId: ConnectorID, operationId: 'replyToMessage' },
  branches: [
    { id: 'sent', description: 'Gmail accepted the reply; its message ID can be empty when an unusable accepted response has no Sent Mail match, while the requested thread ID remains.' },
    { id: 'providerRejected', description: 'Gmail conclusively rejected the reply.', optional: true },
    { id: 'invalidResponse', description: 'Gmail returned an invalid or oversized source message response before sending the reply.', optional: true },
    { id: 'defect', description: 'Local input, connection configuration, or connector definition is invalid.', optional: true },
  ],
  stepDefaults: { executeMethodTimeoutMs: 30_000, executeDurability: 'async', executeRetry: {
    initialIntervalMs: 1000, backoffCoefficient: 2, maximumIntervalMs: 30_000, maximumAttempts: 5, totalDurationMs: 120_000,
  } },
};
