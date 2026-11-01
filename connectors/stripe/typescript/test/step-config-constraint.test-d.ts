// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Call, ConnectionRef, Mutation, MutationStep, Query, QueryStep } from '@superdurable/connectors-sdk';
import type { MockMutation, MockQuery } from '@superdurable/connectors-sdk/connectormock';
import type { Connection, Operations } from '../src/connection.js';
import type { CheckoutSession, CreateACHCheckoutSessionInput, CreateCardCheckoutSessionInput, CreateRefundInput, GetCheckoutSessionInput, GetPaymentIntentInput, PaymentIntent, Refund } from '../src/models.js';
import { newCreateACHCheckoutSessionStep, newCreateCardCheckoutSessionStep, newCreateRefundStep, newGetCheckoutSessionStep, newGetPaymentIntentStep, type CreateRefundStepConfig } from '../src/steps.js';
import type { StripeMock } from '../src/mock.js';

type Exact<Left, Right> = (<Value>() => Value extends Left ? 1 : 2) extends (<Value>() => Value extends Right ? 1 : 2) ?
  ((<Value>() => Value extends Right ? 1 : 2) extends (<Value>() => Value extends Left ? 1 : 2) ? true : false) : false;
type Assert<Value extends true> = Value;
type StepInput = { id: string };

export type PublicTypeAssertions = [
  Assert<Exact<ReturnType<Connection['operations']['createCardCheckoutSession']>, Mutation<CreateCardCheckoutSessionInput, CheckoutSession>>>,
  Assert<Exact<ReturnType<Connection['operations']['createACHCheckoutSession']>, Mutation<CreateACHCheckoutSessionInput, CheckoutSession>>>,
  Assert<Exact<ReturnType<Connection['operations']['getCheckoutSession']>, Query<GetCheckoutSessionInput, CheckoutSession>>>,
  Assert<Exact<ReturnType<Connection['operations']['getPaymentIntent']>, Query<GetPaymentIntentInput, PaymentIntent>>>,
  Assert<Exact<ReturnType<Connection['operations']['createRefund']>, Mutation<CreateRefundInput, Refund>>>,
  Assert<Exact<ReturnType<typeof newCreateCardCheckoutSessionStep<StepInput>>, MutationStep<StepInput, CreateCardCheckoutSessionInput, CheckoutSession>>>,
  Assert<Exact<ReturnType<typeof newCreateACHCheckoutSessionStep<StepInput>>, MutationStep<StepInput, CreateACHCheckoutSessionInput, CheckoutSession>>>,
  Assert<Exact<ReturnType<typeof newGetCheckoutSessionStep<StepInput>>, QueryStep<StepInput, GetCheckoutSessionInput, CheckoutSession>>>,
  Assert<Exact<ReturnType<typeof newGetPaymentIntentStep<StepInput>>, QueryStep<StepInput, GetPaymentIntentInput, PaymentIntent>>>,
  Assert<Exact<ReturnType<typeof newCreateRefundStep<StepInput>>, MutationStep<StepInput, CreateRefundInput, Refund>>>,
  Assert<Exact<ReturnType<StripeMock['createCardCheckoutSession']>, MockMutation<CreateCardCheckoutSessionInput, CheckoutSession>>>,
  Assert<Exact<ReturnType<StripeMock['createACHCheckoutSession']>, MockMutation<CreateACHCheckoutSessionInput, CheckoutSession>>>,
  Assert<Exact<ReturnType<StripeMock['getCheckoutSession']>, MockQuery<GetCheckoutSessionInput, CheckoutSession>>>,
  Assert<Exact<ReturnType<StripeMock['getPaymentIntent']>, MockQuery<GetPaymentIntentInput, PaymentIntent>>>,
  Assert<Exact<ReturnType<StripeMock['createRefund']>, MockMutation<CreateRefundInput, Refund>>>,
];

declare const bareConnection: { reference: ConnectionRef; operations: Operations };
declare const connection: Connection;
declare const call: Call;

// @ts-expect-error A plain operation object is not a Stripe Connection.
const invalidConnection: CreateRefundStepConfig<unknown>['connection'] = bareConnection;
// @ts-expect-error Refund reasons outside the public Stripe union are rejected.
const invalidRefundMapper: CreateRefundStepConfig<unknown>['mapToOperationInput'] = () => ({ paymentIntentId: 'pi_1', amount: 1, reason: 'other' });
const validRefundMapper: CreateRefundStepConfig<unknown>['mapToOperationInput'] = () => ({ paymentIntentId: 'pi_1', amount: 1, reason: 'duplicate' });
// @ts-expect-error The public refund operation rejects provider reasons outside the published union.
const invalidRefundCall = connection.operations.createRefund().invoke(call, { paymentIntentId: 'pi_1', amount: 1, reason: 'other' });
void invalidConnection;
void invalidRefundMapper;
void validRefundMapper;
void invalidRefundCall;
