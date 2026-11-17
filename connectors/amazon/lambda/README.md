# Amazon Lambda connector

Go operations use AWS Signature Version 4:

| Operation | API | Result | Repeat tier |
| --- | --- | --- | --- |
| `getFunction` | [GetFunction](https://docs.aws.amazon.com/lambda/latest/api/API_GetFunction.html) | Name and ARN, or optional `missing` | safe |
| `listFunctions` | [ListFunctions](https://docs.aws.amazon.com/lambda/latest/api/API_ListFunctions.html) | One page of names and ARNs with `nextMarker` | safe |
| `updateFunctionDescription` | [UpdateFunctionConfiguration](https://docs.aws.amazon.com/lambda/latest/api/API_UpdateFunctionConfiguration.html) | Set the complete description and return name and ARN | safe |
| `deleteFunction` | [DeleteFunction](https://docs.aws.amazon.com/lambda/latest/api/API_DeleteFunction.html) | Delete all versions and aliases; an already absent function counts as deleted | safe |

Supply a function name of 1–64 letters, digits, underscores, or hyphens. `listFunctions` accepts a prior `nextMarker` and `maxItems` from 1 to 50; blank marker starts at the beginning and zero item count uses AWS's page size. `updateFunctionDescription` accepts up to 256 characters; blank clears the description. Lambda may apply a configuration update asynchronously; the successful branch means AWS accepted it. Deleting a function is irreversible. Optional branches cover definite provider rejection and malformed responses; rate limits, transport failures, and server errors retry.

In [AWS Lambda Console](https://console.aws.amazon.com/lambda), open **Functions**, select the function, and read its name and ARN from **Function overview**. The console Region selector determines the `region` connection field. In [IAM Console](https://console.aws.amazon.com/iam), open **Users > selected user > Security credentials > Access keys > Create access key**. Grant only the actions used by the Flow: `lambda:GetFunction`, `lambda:ListFunctions`, `lambda:UpdateFunctionConfiguration`, or `lambda:DeleteFunction`. ListFunctions needs an account-wide resource scope; the others can target function ARNs. Copy the access key ID and secret access key into their respective secret fields; the secret value is displayed only once. Set `session_token` as well for temporary credentials. Leave `endpoint` blank to use the regional AWS API. Leave `timeout` and `maxResponseBytes` blank for the displayed defaults. Rotate credentials through IAM when needed.

Runnable examples: [Go](go/examples/function-lookup/README.md), [Python](python/examples/function_lookup/README.md), [TypeScript](typescript/examples/function-lookup/README.md). Go includes the new operations; Python and TypeScript retain `getFunction`. Tests use local fixtures and a mock-backed Dex Flow; live AWS authorization and calls have not been verified.
