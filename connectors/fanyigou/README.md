# Fanyigou connector

`GetAccountBalance` reads the document-translation resource balance and its billing unit. The official `deductType` is 0 for points or 1 for pages; `balance` uses that unit. The connector selects resource package type 1 and returns no account identity or translation content.

At [Fanyigou Open Platform](https://open.fanyigou.com/), complete developer registration and create an application. Copy its appid from Console > Applications and privatekey from profile avatar > Overview > Developer Information > Key into a Dex Web Connection named `fanyigou`. Both are secret fields. The connector uses the documented Bearer privatekey method and generates a random nonce for each form request.

The official [account API and authentication guide](https://open.fanyigou.com/filecore.html) defines the endpoint, response, and provider codes. The checked-in [runnable Flow](go/examples/get-account-balance/) uses a real Dex Worker and a default mock. Live Fanyigou authorization and balance remain unverified.
