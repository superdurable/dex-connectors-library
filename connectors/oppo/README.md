# OPPO Push connector

`GetPushQuota` reads the current day's total, sent, and remaining OPPO Push message counts. It obtains an auth token with a signed request, then sends that token in the quota request header. No message or recipient data is returned.

Create an approved Push application at [OPPO Push](https://push.oppo.com/). In **Applications**, select it and copy its AppKey and MasterSecret into a Dex Web Connection named `oppo`. Both are secret. The operation has no input; leave timeout and response size blank for the displayed defaults.

The endpoint, [auth exchange and quota response](https://storepic.oppomobile.com/openplat/resource/201910/18/OPPO%E6%8E%A8%E9%80%81%E5%B9%B3%E5%8F%B0%E6%9C%8D%E5%8A%A1%E7%AB%AFAPI-V1.9.3.pdf) are documented by OPPO; its [current Push service page](https://open.oppomobile.com/newservice/capability?pagename=push) still links to the server API guidance. The example below uses a real Dex Worker and a default mock. Live OPPO auth and quota behavior remain unverified.

## Example

Run the [GetPushQuota Flow](go/examples/get-push-quota/) with `dexcli dev` and start `OppoPushQuota` with `{}`. The result contains only quota counters.
