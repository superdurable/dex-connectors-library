# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetQueueMetricsInput identifies one queue within one virtual host.
@dataclass(frozen=True, slots=True)
class GetQueueMetricsInput:
    # VirtualHost is the exact RabbitMQ virtual host, commonly /.
    virtual_host: str = _dataclass_field(default='', metadata={'dex_json_name': 'virtualHost'})
    # Name is the exact queue name displayed in the Management UI.
    name: str = ''


setattr(GetQueueMetricsInput, '__module__', 'dex_connectors.rabbitmq.models')


# QueueMetrics contains bounded queue identity and current counts.
@dataclass(frozen=True, slots=True)
class QueueMetrics:
    # VirtualHost is the virtual host returned by RabbitMQ.
    virtual_host: str = _dataclass_field(default='', metadata={'dex_json_name': 'virtualHost'})
    # Name is the queue name returned by RabbitMQ.
    name: str = ''
    # Messages is the total current message count.
    messages: int = 0
    # Consumers is the current consumer count.
    consumers: int = 0
    # State is the provider's current queue state.
    state: str = ''


setattr(QueueMetrics, '__module__', 'dex_connectors.rabbitmq.models')
