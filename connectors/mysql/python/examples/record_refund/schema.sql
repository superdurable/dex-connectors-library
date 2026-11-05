-- Copyright (c) 2026 Super Durable
-- SPDX-License-Identifier: MIT

-- The refund ledger used by the record-refund example. Run it as the table owner on MySQL 8.0.16+ or MariaDB 10.6+.
CREATE TABLE IF NOT EXISTS refund_ledger (
  refund_id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  -- One refund per order across every Flow: another Flow's INSERT becomes a no-op update of this row.
  order_id VARCHAR(100) NOT NULL,
  amount_usd DECIMAL(12, 2) NOT NULL,
  external_reference VARCHAR(200) NULL,
  -- One row per Step execution: a replayed INSERT with the same key changes nothing.
  idempotency_key CHAR(36) CHARACTER SET ascii NOT NULL,
  recorded_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  CONSTRAINT refund_ledger_order_id UNIQUE (order_id),
  CONSTRAINT refund_ledger_idempotency_key UNIQUE (idempotency_key),
  CONSTRAINT refund_ledger_amount_positive CHECK (amount_usd > 0)
) ENGINE = InnoDB;

-- Grant the Dex account only what the example needs, for example:
-- GRANT SELECT, INSERT, UPDATE ON app.refund_ledger TO 'dex_app'@'%';
