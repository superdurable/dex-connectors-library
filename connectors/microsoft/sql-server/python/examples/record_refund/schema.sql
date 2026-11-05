CREATE TABLE dbo.refund_ledger (
  order_id nvarchar(100) NOT NULL,
  amount_usd decimal(12, 2) NOT NULL,
  call_key nvarchar(36) NOT NULL UNIQUE
);
