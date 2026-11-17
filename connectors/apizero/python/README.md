# ApiZero Python connector

`dex-connector-apizero` implements the read-only `getAccountInfo` Query described
in [ApiZero's account documentation](https://apizero.cn/aidocs/user-info). It
returns the wallet balance for a standard Key or the remaining call quota for a
prepaid Key, without including provider messages or secret Key material.

See the [runnable account-information Flow](examples/account_info/) for Worker
and connection setup. Go-recorded conformance scenarios cover request and
decision parity. The checked-in Dex integration test uses a fake provider;
real account authorization and quota values have not been verified.
