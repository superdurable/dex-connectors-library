CREATE TABLE dbo.refund_ledger (
  order_id nvarchar(100) NOT NULL,
  amount_usd decimal(12, 2) NOT NULL,
  call_key nvarchar(36) NOT NULL UNIQUE
);

CREATE TABLE dbo.refund_daily_totals (
  refund_date date NOT NULL PRIMARY KEY,
  total_usd decimal(14, 2) NOT NULL
);
