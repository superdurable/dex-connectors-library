# Amazon Redshift TypeScript package

`AmazonRedshiftClient.getStatementResult()` binds the signed result-page Query. `GetStatementResultInput` holds `statementId` and `nextToken` (blank for the first page). `StatementResultPage` contains `records`, `totalNumRows`, and `nextToken`. See the [connector guide](../README.md) for configuration and IAM permissions. The [runnable Flow](examples/statement-results/README.md) exercises a real Dex Worker with a local provider fixture. Live AWS access has not been verified.
