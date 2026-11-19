# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GoodsCategory, ListRootGoodsCategoriesInput, ListRootGoodsCategoriesOutput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_ROOT_GOODS_CATEGORIES_DEFINITION, PROVIDER_REJECTED

def list_root_goods_categories_listed(value: ListRootGoodsCategoriesOutput, failure: Failure | None = None) -> Case[ListRootGoodsCategoriesOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_root_goods_categories_listed, '__module__', "dex_connectors.xiaoetech.xiaoetechmock")

def list_root_goods_categories_provider_rejected(value: ListRootGoodsCategoriesOutput, failure: Failure | None = None) -> Case[ListRootGoodsCategoriesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_root_goods_categories_provider_rejected, '__module__', "dex_connectors.xiaoetech.xiaoetechmock")

def list_root_goods_categories_invalid_response(value: ListRootGoodsCategoriesOutput, failure: Failure | None = None) -> Case[ListRootGoodsCategoriesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_root_goods_categories_invalid_response, '__module__', "dex_connectors.xiaoetech.xiaoetechmock")

def list_root_goods_categories_defect(value: ListRootGoodsCategoriesOutput, failure: Failure | None = None) -> Case[ListRootGoodsCategoriesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_root_goods_categories_defect, '__module__', "dex_connectors.xiaoetech.xiaoetechmock")

def list_root_goods_categories_retry(failure: Failure, after_seconds: float = 0) -> Case[ListRootGoodsCategoriesOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_root_goods_categories_retry, '__module__', "dex_connectors.xiaoetech.xiaoetechmock")

def list_root_goods_categories_sample_root_categories() -> Case[ListRootGoodsCategoriesOutput]:
    return Case.branch_result(LISTED, ListRootGoodsCategoriesOutput(categories=(GoodsCategory(id=16510, title='Sample food', has_children=True), GoodsCategory(id=18237, title='Sample beauty', has_children=False))))

setattr(list_root_goods_categories_sample_root_categories, '__module__', "dex_connectors.xiaoetech.xiaoetechmock")


class Mock:

    def __init__(self, connection_name: str = "xiaoetech") -> None:
        self.list_root_goods_categories_script: Query[ListRootGoodsCategoriesInput, ListRootGoodsCategoriesOutput] = Query(LIST_ROOT_GOODS_CATEGORIES_DEFINITION)
        self.list_root_goods_categories_script.default(list_root_goods_categories_sample_root_categories())
        self._connection = new_connection_with_operations(
            list_root_goods_categories=self.list_root_goods_categories_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.xiaoetech.xiaoetechmock")

__all__ = [
    "Mock",
    "list_root_goods_categories_listed",
    "list_root_goods_categories_provider_rejected",
    "list_root_goods_categories_invalid_response",
    "list_root_goods_categories_defect",
    "list_root_goods_categories_retry",
    "list_root_goods_categories_sample_root_categories",
]
