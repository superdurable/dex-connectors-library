// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Connection as GenericConnection } from '../src/generated/connection.js';
import type { GetMessageStepConfig, SendMessageStepConfig } from '../src/steps.js';

declare const genericConnection: GenericConnection;

// @ts-expect-error Gmail Steps require the typed GmailConnection.
const invalidReadConnection: GetMessageStepConfig<unknown>['connection'] = genericConnection;
// @ts-expect-error Gmail Steps require the typed GmailConnection.
const invalidSendConnection: SendMessageStepConfig<unknown>['connection'] = genericConnection;
void invalidReadConnection;
void invalidSendConnection;
