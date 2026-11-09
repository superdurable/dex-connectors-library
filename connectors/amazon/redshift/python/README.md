# Amazon Redshift Python package

`Client.get_statement_result()` binds the signed result-page Query. `GetStatementResultInput` holds `statement_id` and `next_token` (blank for the first page). `StatementResultPage` contains `records`, `total_num_rows`, and `next_token`. See the [connector guide](../README.md) for configuration and IAM permissions. The [runnable Flow](examples/statement_results/README.md) exercises a real Dex Worker with a local provider fixture. Live AWS access has not been verified.
