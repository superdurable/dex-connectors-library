// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectionRef } from '@superdurable/connectors-sdk';
import type { Operations } from '../src/connection.js';
import type { CreateRefundStepConfig } from '../src/steps.js';

declare const bareConnection: { reference: ConnectionRef; operations: Operations };

// @ts-expect-error A plain operation object is not a Stripe Connection.
const invalidConnection: CreateRefundStepConfig<unknown>['connection'] = bareConnection;
// @ts-expect-error Refund reasons outside the public Stripe union are rejected.
const invalidRefundMapper: CreateRefundStepConfig<unknown>['mapToOperationInput'] = () => ({ paymentIntentId: 'pi_1', amount: 1, reason: 'other' });
const validRefundMapper: CreateRefundStepConfig<unknown>['mapToOperationInput'] = () => ({ paymentIntentId: 'pi_1', amount: 1, reason: 'duplicate' });
void invalidConnection;
void invalidRefundMapper;
void validRefundMapper;
