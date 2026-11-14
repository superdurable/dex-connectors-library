# Chuanglan 253 domestic SMS balance

The Go connector performs the official read-only [balance lookup](https://doc.chuanglan.com/docs/4XVHFKRMBD6M4T9C). It returns only the number of SMS messages remaining in the connected API account. The API account and password are sent in the HTTPS JSON request body and never returned in Flow output.

The default endpoint is the East China `smssh1` node. Chuanglan [node guidance](https://chuanglan.apifox.cn/doc-357766) says to use the hostname assigned to the account; configure that origin in Dex Web Connections if different. The console may require the worker's outbound IP to be on the account whitelist. The provider does not publish a balance-query rate limit.

`getBalance` selects `fetched`, `providerRejected`, `invalidResponse`, or `defect`. HTTP 429, temporary 5xx, and transport failures retry because the lookup is read-only. Optional branches fail the example Flow unless wired by an application.

[Run the balance Flow](go/examples/balance/README.md). Its checked-in mock is synthetic; live balance lookup has not been verified without a user-controlled account in Dex Web Connections.
