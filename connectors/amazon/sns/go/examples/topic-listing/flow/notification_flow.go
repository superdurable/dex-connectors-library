// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package topiclisting

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/amazon/sns/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// NotificationFlowType is the stable Flow identity for one SNS notification.
	NotificationFlowType = "AmazonSNSNotificationIntake"
	// NotificationBindingName identifies the SNS notification Trigger binding.
	NotificationBindingName = "sns-notification"
)

var notificationAttribute = dex.DefineAttribute[sns.SNSNotification]("sns-notification")

// NotificationFlow records one verified SNS notification.
type NotificationFlow struct{ dex.FlowDefaults }

// NewNotificationFlow creates the notification intake Flow.
func NewNotificationFlow() *NotificationFlow { return &NotificationFlow{} }

// GetFlowType returns the stable notification Flow identity.
func (*NotificationFlow) GetFlowType() string { return NotificationFlowType }

// GetPersistenceSchema registers the verified notification.
func (*NotificationFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{notificationAttribute}}
}

// GetConnectorTriggerBindings declares the SNS notification binding.
func (*NotificationFlow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{sns.DefineNotificationReceivedTriggerBinding(sns.NotificationReceivedTriggerBindingConfig{
		ConnectionName: ConnectionName, BindingName: NotificationBindingName,
	})}
}

// GetRPCs exposes the verified notification in Dex Web.
func (flow *NotificationFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the verified notification when available.
//
// dex:field attribute-key:sns-notification value-type:json editable:false description:"Verified SNS notification"
func (*NotificationFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	notification, err := readNotification(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sns-notification": notification}}, nil
}

// GetDexDisplay returns the verified notification when available.
//
// dex:field attribute-key:sns-notification value-type:json editable:false description:"Verified SNS notification"
func (*NotificationFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	notification, err := readNotification(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sns-notification": notification}}, nil
}

func readNotification(ctx dex.Context) (sns.SNSNotification, error) {
	notification, err := notificationAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return sns.SNSNotification{}, nil
	}
	return notification, err
}

// GetSteps records the notification and completes the Flow.
func (*NotificationFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStartStep(recordNotification{})}
}

// AcceptEveryNotification admits every event selected by the binding's topic.
func AcceptEveryNotification(sdkgo.TriggerEvent[sns.SNSNotification]) bool { return true }

// ResolveNotificationFlowID gives SNS retries the same Flow identity.
func ResolveNotificationFlowID(event sdkgo.TriggerEvent[sns.SNSNotification]) string {
	return "sns-notification-" + event.ID
}

// MapNotificationToFlowInput keeps the verified notification as the Flow input.
func MapNotificationToFlowInput(event sdkgo.TriggerEvent[sns.SNSNotification]) sns.SNSNotification {
	return event.Payload
}

// dex:group group-id:notification group-label:"Notification"
// dex:explanation text:"Record the verified SNS notification and complete the Flow."
type recordNotification struct{ dex.StepDefaults }

func (recordNotification) GetStepType() string { return "RecordSNSNotification" }
func (recordNotification) WaitFor(dex.Context, sns.SNSNotification) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (recordNotification) Execute(ctx dex.Context, notification sns.SNSNotification) (*dex.StepDecision, error) {
	if err := notificationAttribute.Set(ctx, notification); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(notification), nil
}

var _ dex.Flow = (*NotificationFlow)(nil)
