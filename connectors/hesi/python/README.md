# Hesi Python connector

`dex-connector-hesi` implements the `listDepartments` Query. Configure AppKey and
AppSecurity in Dex Web Connections, along with the documented enterprise API origin
when it differs from the standalone-platform default. Each attempt exchanges the
credentials for a short-lived token and reads one department page. The token is
never returned or persisted.

The input selects zero-based `start` and a `count` from 1 to 100; zero count uses
20. The output includes department ID, name, code, active state, total count,
and next offset. The optional `providerRejected`, `invalidResponse`, and `defect`
branches remain unwired in the [runnable department directory Flow](examples/department_directory/).

Seven Go-recorded scenarios verify request and decision parity. The example's
integration test runs a real Dex Worker and Client against a fake Hesi provider,
including a retry after HTTP 503. Live Hesi authorization, tenant origin
selection, and department pagination remain unverified without an approved
enterprise account.
