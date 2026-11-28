// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { newConnection, newConnectionWithOperations } from '../src/index.js';
import { Mock } from '../src/mock.js';

it('retains getPerson-only connection bindings', () => {
  const bound = new Mock('personio').connection();
  const original = bound.operations.getPerson();
  for (const bind of [newConnection, newConnectionWithOperations]) {
    const connection = bind({ getPerson: () => original }, bound.reference);
    expect(connection.operations.getPerson()).toBe(original);
    expect(() => connection.operations.listPersons()).toThrow('listPersons is not bound');
  }
});
