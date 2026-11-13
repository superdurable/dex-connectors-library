# Bmob class object count

The Go connector runs a read-only count for one class in a Bmob application. The [official REST reference](https://doc.bmobapp.com/data/restful/develop_doc/) documents `GET /1/classes/<TableName>?count=1&limit=0`, returning `{"results":[],"count":N}`. The connector discards the response body after decoding this exact shape and outputs only the count. It never requests business record fields, user records, or a Master Key.

An application-specific HTTPS API domain is required. The [domain guide](https://doc.bmobapp.com/other/domain/) locates it in **控制台 > 应用设置 > 域名管理 > API域名**. The [quick start](https://doc.bmobapp.com/data/restful/) locates Application ID and REST API Key in **设置 > 应用密钥**. The connector sends both only as HTTPS headers and keeps them in Dex credentials. Bmob's [REST error reference](https://doc.bmobapp.com/other/error_code/) defines 401 as unauthorized, 500 as temporary server failure, and class errors as 400/404.

See the [runnable Dex Flow](go/examples/class-object-count/README.md). Mock tests do not verify a live application's domain, credentials, class permissions, or count response.
