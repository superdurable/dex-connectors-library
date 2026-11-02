// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString } from '@superdurable/connectors-sdk';
import { BambooHRClient } from '../src/client.js';
import { buildTimeOffRequestFilter } from '../src/operations.js';

const credentials = { resolve: async () => ({ apiKey: new SecretString('0123456789abcdef0123456789abcdef01234567') }) };

describe('BambooHR client', () => {
  it('rejects a company domain that could redirect credentials', () => {
    expect(() => new BambooHRClient({ companyDomain: 'other.example.com' }, credentials)).toThrow();
    expect(() => new BambooHRClient({ companyDomain: 'acme/path' }, credentials)).toThrow();
  });

  it('builds an overlap filter with lowercase provider statuses', () => {
    expect(buildTimeOffRequestFilter({ startDate: '2026-10-13', endDate: '2026-10-26', employeeId: '123', statuses: ['REQUESTED', 'APPROVED'] }))
      .toBe("startDate le '2026-10-26' and endDate ge '2026-10-13' and employeeId eq 123 and status in ('requested', 'approved')");
  });
});
