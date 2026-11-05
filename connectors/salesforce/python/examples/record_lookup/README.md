# Salesforce record lookup example

This Flow reads one Salesforce record through the static `salesforce-crm` Dex Web connection. Configure that connection using the Salesforce authorization guide, then start `python -m examples.record_lookup.worker` from this package. Start a Flow of type `SalesforceRecordLookup` with a `GetRecordInput` such as `{"sObjectType":"Contact","recordId":"003RM000001AbcdYAC","fields":["Id","Email"]}`.

The integration test starts `dexcli dev` and exercises the checked-in Flow through a real Dex Worker and Client with an inert local Salesforce HTTP fake. Live Salesforce authorization remains unverified.
