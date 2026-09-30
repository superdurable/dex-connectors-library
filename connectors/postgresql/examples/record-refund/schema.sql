-- Copyright (c) 2026 Super Durable
-- SPDX-License-Identifier: MIT

-- The refund ledger used by the record-refund example. Run it as the table owner.
CREATE TABLE IF NOT EXISTS refund_ledger (
  refund_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  -- One refund per order across every Flow: a second Flow's INSERT fails with SQLSTATE 23505.
  order_id text NOT NULL UNIQUE,
  amount_usd numeric(12, 2) NOT NULL CHECK (amount_usd > 0),
  external_reference text,
  -- One row per Step execution: a replayed INSERT with the same key does nothing.
  idempotency_key uuid NOT NULL UNIQUE,
  recorded_at timestamptz NOT NULL DEFAULT now()
);

-- Grant the Dex login role only what the example needs, for example:
-- GRANT SELECT, INSERT ON refund_ledger TO dex_app;
