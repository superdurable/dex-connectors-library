# NavInfo LBS connector

`LookupIPLocation` calls the official [IP定位 REST API](https://lbs.navinfo.com/docs/webapi/ipcity.html) over HTTPS and returns only province, city, and administrative code for one IP address. It does not return or store the provider's full response. The [NavInfo platform](https://lbs.navinfo.com/docs/webapi/summary.html) is operated by 北京四维图新科技股份有限公司.

Open the [IP定位 API documentation](https://lbs.navinfo.com/docs/webapi/ipcity.html), click 申请 beside the AK parameter, sign in, and obtain a REST API AK with IP定位 access. Copy this secret into Dex Web Connections. The AK is sent in the HTTPS query string; redirects are disabled and errors omit the request URL and AK. Leave timeout and maxResponseBytes blank for displayed defaults.

Start the [runnable Flow](go/examples/lookup-ip-location/) with `{"ipAddress":"8.8.8.8"}`. Its default mock returns a Beijing example. Live AK authorization and location results have not been verified with a provider account.
