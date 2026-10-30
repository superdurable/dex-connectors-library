-- Copyright (c) 2026 Super Durable
-- SPDX-License-Identifier: MIT

-- The refund ledger used by the record-refund example. Run it as the table owner in the application
-- database on SQL Server 2016 or later, Azure SQL Database, or Azure SQL Managed Instance.
CREATE TABLE dbo.refund_ledger (
  refund_id bigint IDENTITY(1, 1) NOT NULL CONSTRAINT pk_refund_ledger PRIMARY KEY,
  -- One refund per order across every Flow: another Flow's INSERT finds this row and inserts nothing.
  order_id nvarchar(100) NOT NULL CONSTRAINT uq_refund_ledger_order_id UNIQUE,
  amount_usd decimal(12, 2) NOT NULL CONSTRAINT ck_refund_ledger_amount_positive CHECK (amount_usd > 0),
  external_reference nvarchar(200) NULL,
  -- One row per Step execution: a replayed INSERT with the same key inserts nothing.
  idempotency_key uniqueidentifier NOT NULL CONSTRAINT uq_refund_ledger_idempotency_key UNIQUE,
  recorded_at datetime2(6) NOT NULL CONSTRAINT df_refund_ledger_recorded_at DEFAULT SYSUTCDATETIME()
);

-- Grant the Dex user only what the example needs, for example:
-- GRANT SELECT, INSERT ON dbo.refund_ledger TO dex_app;
