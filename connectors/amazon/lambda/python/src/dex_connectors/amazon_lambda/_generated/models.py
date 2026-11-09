# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetFunctionInput identifies one Lambda function in the configured Region.
@dataclass(frozen=True, slots=True)
class GetFunctionInput:
    function_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'functionName'})


setattr(GetFunctionInput, '__module__', 'dex_connectors.amazon_lambda.models')


# FunctionDetails returns the stable identity of a Lambda function.
@dataclass(frozen=True, slots=True)
class FunctionDetails:
    name: str = ''
    arn: str = ''


setattr(FunctionDetails, '__module__', 'dex_connectors.amazon_lambda.models')
