// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectionRef } from '@superdurable/connectors-sdk';
import { Connection, type Operations } from './generated/connection.js';

export { Connection };

type AvailableOperations = Pick<Operations, 'getPerson'> & Partial<Pick<Operations, 'listPersons'>>;

/** Bind the available Personio queries to a named connection. */
export function newConnection(client: AvailableOperations, reference: ConnectionRef): Connection {
  return newConnectionWithOperations(client, reference);
}

/** Retain single-person bindings and require a supplied page query when binding a listPersons Step. */
export function newConnectionWithOperations(operations: AvailableOperations, reference: ConnectionRef): Connection {
  if (!operations || typeof operations.getPerson !== 'function') throw new TypeError('Personio getPerson operation is required');
  if (typeof operations.listPersons === 'function') return new Connection(reference, operations as Operations);
  return new Connection(reference, {
    getPerson: () => operations.getPerson(),
    listPersons: () => { throw new TypeError('listPersons is not bound to this Personio connection'); },
  });
}
