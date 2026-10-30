// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expectTypeOf, it } from 'vitest';
import type { AsyncContext, Context } from '@superdurable/dex';

function inspectReleasedContext(context: Context, asyncContext: AsyncContext): void {
  expectTypeOf(context.deadline).toEqualTypeOf<Date | undefined>();
  expectTypeOf(context.flowStartedAt).toEqualTypeOf<Date>();
  expectTypeOf(asyncContext.recordHeartbeat(undefined)).toEqualTypeOf<Promise<void>>();
}

describe('released Dex 1.28 context contract', () => {
  it('uses the public deadline and flow start field names', () => {
    expectTypeOf(inspectReleasedContext).toBeFunction();
  });
});
