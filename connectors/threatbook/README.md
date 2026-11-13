# ThreatBook connector

This connector checks one inbound source IP through the [ThreatBook IP Reputation API](https://x.threatbook.com/apiDocs/ip/reputation). It returns the malicious verdict, confidence level, severity, and up to twenty threat-type labels. The provider specifically designs this endpoint for inbound access sources; use business logs and false-positive tolerance before acting on the verdict. ThreatBook Cloud API is operated by [北京微步在线科技有限公司](https://x.threatbook.com/apiDocs/ip/reputation).

Get a Cloud API Key from [我的API > 我的KEY](https://x.threatbook.com/apiDocs/start/get_api_key), ensure IP信誉 permission, and bind the Dex outbound public IP in the provider's allowlist. Save the Key in Dex Web Connections. The provider requires it in an HTTPS query parameter, so redirects are disabled and the Key, raw URL, and provider message never enter Flow output or failures. A request may consume plan quota; retries are bounded.

Run the [example Flow](go/examples/check-ip-reputation/) with the generated mock or a user-controlled connection. Live verdicts, allowlist behavior, and quota use are unverified without a user-controlled Key.
