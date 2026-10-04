// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package paidorder demonstrates the QuickBooks connector in one Flow started from Dex Web Start
// Flow: for an order the customer has already paid, find the customer by email or create them,
// look for an invoice already raised under the order ID, raise one with exact decimal lines when
// there is none, record the payment against it, email the paid invoice as a receipt, and read it
// back to confirm nothing is due.
package paidorder

import (
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/intuit/quickbooks"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "QuickBooksPaidOrderInvoice"
	// ConnectionName is the static Dex Web connection for QuickBooks Online.
	ConnectionName = "quickbooks-company"

	recordPaidOrderStepType         = "RecordPaidOrder"
	findOrderCustomerStepType       = "FindOrderCustomer"
	prepareCustomerCreationStepType = "PrepareCustomerCreation"
	createOrderCustomerStepType     = "CreateOrderCustomer"
	recordFoundCustomerStepType     = "RecordFoundCustomer"
	recordCreatedCustomerStepType   = "RecordCreatedCustomer"
	findOrderInvoiceStepType        = "FindOrderInvoice"
	chooseOrderInvoiceStepType      = "ChooseOrderInvoice"
	raiseOrderInvoiceStepType       = "RaiseOrderInvoice"
	prepareOrderPaymentStepType     = "PrepareOrderPayment"
	recordOrderPaymentStepType      = "RecordOrderPayment"
	prepareInvoiceDeliveryStepType  = "PrepareInvoiceDelivery"
	sendPaidInvoiceStepType         = "SendPaidInvoice"
	readBackPaidInvoiceStepType     = "ReadBackPaidInvoice"
	completePaidOrderStepType       = "CompletePaidOrder"

	// orderInvoiceSearchResults bounds the invoices read for one order number.
	orderInvoiceSearchResults = 10
	maximumOrderLines         = 50
)

var (
	paidOrderAttribute       = dex.DefineAttribute[PaidOrder]("quickbooks-paid-order")
	orderCustomerAttribute   = dex.DefineAttribute[OrderCustomer]("quickbooks-order-customer")
	raisedInvoiceAttribute   = dex.DefineAttribute[sdkgo.MutationResult[quickbooks.CreateInvoiceOutput]]("quickbooks-order-raised-invoice")
	recordedPaymentAttribute = dex.DefineAttribute[sdkgo.MutationResult[quickbooks.Payment]]("quickbooks-order-recorded-payment")
	orderOutcomeAttribute    = dex.DefineAttribute[PaidOrderOutcome]("quickbooks-paid-order-outcome")

	// orderIDPattern fits QuickBooks's 21-character DocNumber, which the order ID becomes.
	orderIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,20}$`)
)

// Input is the paid order entered in Dex Web Start Flow.
type Input struct {
	// OrderID is the application's order ID, such as ORDER-1042; it becomes the invoice number.
	OrderID string `json:"orderId"`
	// CustomerEmail is the customer's email address, used to find or create the customer and to send the invoice.
	CustomerEmail string `json:"customerEmail"`
	// CustomerDisplayName is the display name for a customer the Flow creates; it must be unique in QuickBooks.
	CustomerDisplayName string `json:"customerDisplayName"`
	// CustomerCompanyName is the optional company name for a customer the Flow creates.
	CustomerCompanyName string `json:"customerCompanyName,omitempty"`
	// InvoiceDate is the invoice date, YYYY-MM-DD.
	InvoiceDate string `json:"invoiceDate"`
	// DueDate is the invoice due date, YYYY-MM-DD.
	DueDate string `json:"dueDate"`
	// Lines are the order lines, with prices as exact decimal strings.
	Lines []OrderLine `json:"lines"`
	// PaidOn is the date the customer paid, YYYY-MM-DD.
	PaidOn string `json:"paidOn"`
	// DepositAccountID is the optional Id of the bank account that received the money; blank uses Undeposited Funds.
	DepositAccountID string `json:"depositAccountId,omitempty"`
	// PaymentReference is an optional payment reference, such as the card charge ID, at most 21 characters.
	PaymentReference string `json:"paymentReference,omitempty"`
}

// OrderLine is one order line.
type OrderLine struct {
	// ItemID is the QuickBooks product or service item's Id, such as 1.
	ItemID string `json:"itemId"`
	// Description is the line description shown on the invoice.
	Description string `json:"description"`
	// Quantity is the quantity, an exact decimal string such as 2.
	Quantity quickbooks.Decimal `json:"quantity"`
	// UnitPrice is the unit price, an exact decimal string such as 49.95.
	UnitPrice quickbooks.Decimal `json:"unitPrice"`
	// TaxCodeID optionally sets the line's QuickBooks tax code, such as TAX or NON.
	TaxCodeID string `json:"taxCodeId,omitempty"`
}

// PaidOrder is the validated order every later Step reads.
type PaidOrder Input

// OrderCustomer is the QuickBooks customer the order belongs to.
type OrderCustomer struct {
	// Customer is the customer as QuickBooks returned it.
	Customer quickbooks.Customer `json:"customer"`
	// IsCreated reports that this Flow created the customer.
	IsCreated bool `json:"isCreated"`
}

// NewCustomerRequest is the customer to create when no customer has the order's email address.
type NewCustomerRequest struct {
	// DisplayName is the unique display name.
	DisplayName string `json:"displayName"`
	// EmailAddress is the customer's email address.
	EmailAddress string `json:"emailAddress"`
	// CompanyName is the optional company name.
	CompanyName string `json:"companyName,omitempty"`
}

// OrderInvoiceLookup asks QuickBooks for an invoice already raised under the order ID.
type OrderInvoiceLookup struct {
	// CustomerID is the customer's Id.
	CustomerID string `json:"customerId"`
	// OrderID is the invoice number to look for.
	OrderID string `json:"orderId"`
}

// OrderInvoiceRequest is the invoice to raise for the order.
type OrderInvoiceRequest struct {
	// CustomerID is the customer's Id.
	CustomerID string `json:"customerId"`
	// Order is the validated order.
	Order PaidOrder `json:"order"`
}

// OrderPayment is the payment to record against the order's invoice.
type OrderPayment struct {
	// CustomerID is the paying customer's Id.
	CustomerID string `json:"customerId"`
	// InvoiceID is the invoice to pay.
	InvoiceID string `json:"invoiceId"`
	// Amount is the exact balance still due on the invoice.
	Amount quickbooks.Decimal `json:"amount"`
	// PaidOn is the payment date.
	PaidOn string `json:"paidOn"`
	// DepositAccountID is the optional receiving account's Id.
	DepositAccountID string `json:"depositAccountId,omitempty"`
	// Reference is the optional payment reference.
	Reference string `json:"reference,omitempty"`
}

// InvoiceDelivery is the paid invoice to email as the customer's receipt.
type InvoiceDelivery struct {
	// InvoiceID is the invoice to send.
	InvoiceID string `json:"invoiceId"`
	// SendTo is the customer's email address.
	SendTo string `json:"sendTo"`
}

// PaidOrderAction is the Flow's terminal business outcome.
type PaidOrderAction string

const (
	// OrderInvoicedAndPaid means the Flow raised the invoice, recorded the payment, and sent the invoice.
	OrderInvoicedAndPaid PaidOrderAction = "invoicedAndPaid"
	// OrderExistingInvoicePaid means an open invoice for the order existed and the Flow paid and sent it.
	OrderExistingInvoicePaid PaidOrderAction = "existingInvoicePaid"
	// OrderAlreadySettled means a paid invoice for the order existed, so the Flow wrote nothing.
	OrderAlreadySettled PaidOrderAction = "alreadySettled"
)

// PaidOrderOutcome is the Flow result and the value of its outcome Attribute.
type PaidOrderOutcome struct {
	// Action is what the Flow did.
	Action PaidOrderAction `json:"action"`
	// OrderID is the order.
	OrderID string `json:"orderId"`
	// CustomerID is the customer's Id.
	CustomerID string `json:"customerId"`
	// IsCustomerCreated reports that the Flow created the customer.
	IsCustomerCreated bool `json:"isCustomerCreated"`
	// InvoiceID is the order's invoice.
	InvoiceID string `json:"invoiceId,omitempty"`
	// DocNumber is the invoice number.
	DocNumber string `json:"docNumber,omitempty"`
	// TotalAmount is the invoice total, exactly as QuickBooks stores it.
	TotalAmount quickbooks.Decimal `json:"totalAmount,omitempty"`
	// Balance is the amount still due as last read.
	Balance quickbooks.Decimal `json:"balance,omitempty"`
	// EmailStatus is the invoice's email state as last read.
	EmailStatus quickbooks.EmailStatus `json:"emailStatus,omitempty"`
	// PaymentID is the payment the Flow recorded.
	PaymentID string `json:"paymentId,omitempty"`
	// IsFullyPaid reports that the read-back invoice has a zero balance.
	IsFullyPaid bool `json:"isFullyPaid"`
}

// Flow invoices one paid order in QuickBooks Online and records its payment.
type Flow struct {
	dex.FlowDefaults
	connection quickbooks.Connection
}

// NewFlow binds the QuickBooks Connection at registration time.
func NewFlow(connection quickbooks.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and QuickBooks connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordPaidOrder{}),
		dex.DefineStep(quickbooks.NewFindCustomerStep(quickbooks.FindCustomerStepConfig[PaidOrder]{
			StepType: findOrderCustomerStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "quickbooks", GroupLabel: "QuickBooks Online",
				Explanation: "Find the active customer with the order's email address.",
			},
			Connection: flow.connection, MapToOperationInput: MapToFindCustomerInput,
			Found:    sdkgo.GoTo(recordFoundCustomer{}),
			NotFound: sdkgo.GoTo(prepareCustomerCreation{}),
		})),
		dex.DefineStep(prepareCustomerCreation{}),
		dex.DefineStep(quickbooks.NewCreateCustomerStep(quickbooks.CreateCustomerStepConfig[NewCustomerRequest]{
			StepType: createOrderCustomerStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "quickbooks", GroupLabel: "QuickBooks Online",
				Explanation: "Create the customer under a Step-derived requestid, so a retry returns the same customer.",
			},
			Connection: flow.connection, MapToOperationInput: MapToCreateCustomerInput,
			Created: sdkgo.GoTo(recordCreatedCustomer{}),
		})),
		dex.DefineStep(recordFoundCustomer{}),
		dex.DefineStep(recordCreatedCustomer{}),
		dex.DefineStep(quickbooks.NewListInvoicesStep(quickbooks.ListInvoicesStepConfig[OrderInvoiceLookup]{
			StepType: findOrderInvoiceStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "quickbooks", GroupLabel: "QuickBooks Online",
				Explanation: "List the customer's invoices whose number is the order ID.",
			},
			Connection: flow.connection, MapToOperationInput: MapToListInvoicesInput,
			Listed: sdkgo.GoTo(chooseOrderInvoice{}),
		})),
		dex.DefineStep(chooseOrderInvoice{}),
		dex.DefineStep(quickbooks.NewCreateInvoiceStep(quickbooks.CreateInvoiceStepConfig[OrderInvoiceRequest]{
			StepType: raiseOrderInvoiceStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "quickbooks", GroupLabel: "QuickBooks Online",
				Explanation: "Raise the invoice numbered with the order ID under a Step-derived requestid, so a retry returns the same invoice.",
			},
			Connection: flow.connection, MapToOperationInput: MapToCreateInvoiceInput,
			ResultAttribute: &raisedInvoiceAttribute,
			Created:         sdkgo.GoTo(prepareOrderPayment{}),
		})),
		dex.DefineStep(prepareOrderPayment{}),
		dex.DefineStep(quickbooks.NewRecordPaymentStep(quickbooks.RecordPaymentStepConfig[OrderPayment]{
			StepType: recordOrderPaymentStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "quickbooks", GroupLabel: "QuickBooks Online",
				Explanation: "Record the order's payment against the invoice under a Step-derived requestid, so a retry never pays twice.",
			},
			Connection: flow.connection, MapToOperationInput: MapToRecordPaymentInput,
			ResultAttribute: &recordedPaymentAttribute,
			Recorded:        sdkgo.GoTo(prepareInvoiceDelivery{}),
		})),
		dex.DefineStep(prepareInvoiceDelivery{}),
		dex.DefineStep(quickbooks.NewSendInvoiceStep(quickbooks.SendInvoiceStepConfig[InvoiceDelivery]{
			StepType: sendPaidInvoiceStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "quickbooks", GroupLabel: "QuickBooks Online",
				Explanation: "Email the paid invoice to the customer as a receipt under a Step-derived requestid.",
			},
			Connection: flow.connection, MapToOperationInput: MapToSendInvoiceInput,
			Sent: sdkgo.GoTo(sdkgo.StepRef[quickbooks.SendInvoiceResult](readBackPaidInvoiceStepType)),
		})),
		dex.DefineStep(quickbooks.NewGetInvoiceStep(quickbooks.GetInvoiceStepConfig[quickbooks.SendInvoiceResult]{
			StepType: readBackPaidInvoiceStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "quickbooks", GroupLabel: "QuickBooks Online",
				Explanation: "Read the invoice back to confirm the payment landed and nothing is due.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetInvoiceInput,
			Found: sdkgo.GoTo(completePaidOrder{}),
		})),
		dex.DefineStep(completePaidOrder{}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the order, customer, write Results, and outcome Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{
		paidOrderAttribute, orderCustomerAttribute, raisedInvoiceAttribute, recordedPaymentAttribute, orderOutcomeAttribute,
	}}
}

// GetDexSummary returns the order and its outcome.
//
// dex:field attribute-key:quickbooks-paid-order value-type:json editable:false description:"Paid order"
// dex:field attribute-key:quickbooks-paid-order-outcome value-type:json editable:false description:"QuickBooks outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	order, outcome, err := orderInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"quickbooks-paid-order": order, "quickbooks-paid-order-outcome": outcome}}, nil
}

// GetDexDisplay returns the order and its invoice, payment, and delivery outcome.
//
// dex:field attribute-key:quickbooks-paid-order value-type:json editable:false description:"Order, customer, lines, and payment"
// dex:field attribute-key:quickbooks-paid-order-outcome value-type:json editable:false description:"Action, invoice, payment, and email status"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	order, outcome, err := orderInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"quickbooks-paid-order": order, "quickbooks-paid-order-outcome": outcome}}, nil
}

// MapToFindCustomerInput looks up the active customer with the order's email address.
func MapToFindCustomerInput(order PaidOrder) quickbooks.FindCustomerInput {
	return quickbooks.FindCustomerInput{EmailAddress: order.CustomerEmail}
}

// MapToCreateCustomerInput creates the customer with the order's display name and email address.
func MapToCreateCustomerInput(request NewCustomerRequest) quickbooks.CreateCustomerInput {
	return quickbooks.CreateCustomerInput{
		DisplayName: request.DisplayName, EmailAddress: request.EmailAddress, CompanyName: request.CompanyName,
	}
}

// MapToListInvoicesInput finds the customer's invoices numbered with the order ID.
func MapToListInvoicesInput(lookup OrderInvoiceLookup) quickbooks.ListInvoicesInput {
	return quickbooks.ListInvoicesInput{CustomerID: lookup.CustomerID, DocNumber: lookup.OrderID, MaxResults: orderInvoiceSearchResults}
}

// MapToCreateInvoiceInput raises the invoice numbered with the order ID and billed to the customer's email.
func MapToCreateInvoiceInput(request OrderInvoiceRequest) quickbooks.CreateInvoiceInput {
	order := request.Order
	lines := make([]quickbooks.InvoiceLineInput, 0, len(order.Lines))
	for _, line := range order.Lines {
		lines = append(lines, quickbooks.InvoiceLineInput{
			ItemID: line.ItemID, Description: line.Description, Quantity: line.Quantity, UnitPrice: line.UnitPrice, TaxCodeID: line.TaxCodeID,
		})
	}
	return quickbooks.CreateInvoiceInput{
		CustomerID: request.CustomerID, Lines: lines, TxnDate: order.InvoiceDate, DueDate: order.DueDate,
		DocNumber: order.OrderID, BillEmail: order.CustomerEmail, PrivateNote: "Order " + order.OrderID,
	}
}

// MapToRecordPaymentInput pays the invoice's exact balance.
func MapToRecordPaymentInput(payment OrderPayment) quickbooks.RecordPaymentInput {
	return quickbooks.RecordPaymentInput{
		CustomerID: payment.CustomerID, InvoiceID: payment.InvoiceID, Amount: payment.Amount, TxnDate: payment.PaidOn,
		DepositAccountID: payment.DepositAccountID, PaymentReferenceNumber: payment.Reference,
	}
}

// MapToSendInvoiceInput emails the invoice to the customer's address.
func MapToSendInvoiceInput(delivery InvoiceDelivery) quickbooks.SendInvoiceInput {
	return quickbooks.SendInvoiceInput{InvoiceID: delivery.InvoiceID, SendTo: delivery.SendTo}
}

// MapToGetInvoiceInput reads back the invoice that was sent.
func MapToGetInvoiceInput(result quickbooks.SendInvoiceResult) quickbooks.GetInvoiceInput {
	return quickbooks.GetInvoiceInput{InvoiceID: result.Value.InvoiceID}
}

func orderInspection(ctx dex.Context) (PaidOrder, PaidOrderOutcome, error) {
	order, err := optionalAttribute(ctx, paidOrderAttribute)
	if err != nil {
		return PaidOrder{}, PaidOrderOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, orderOutcomeAttribute)
	if err != nil {
		return PaidOrder{}, PaidOrderOutcome{}, err
	}
	return order, outcome, nil
}

func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		var zero T
		return zero, nil
	}
	return value, err
}

// dex:group group-id:order group-label:"Paid order"
// dex:explanation text:"Validate the paid order and record it before calling QuickBooks."
type recordPaidOrder struct {
	dex.StepDefaults
}

func (recordPaidOrder) GetStepType() string { return recordPaidOrderStepType }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordPaidOrder) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordPaidOrder) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	order, err := BuildPaidOrder(input)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := paidOrderAttribute.Set(ctx, order); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[PaidOrder](findOrderCustomerStepType), order), nil
}

// dex:group group-id:order group-label:"Paid order"
// dex:explanation text:"Prepare a new customer from the order when no customer has its email address."
type prepareCustomerCreation struct {
	dex.StepDefaultsNoWaitFor[quickbooks.FindCustomerResult]
}

func (prepareCustomerCreation) GetStepType() string { return prepareCustomerCreationStepType }

func (prepareCustomerCreation) Execute(ctx dex.Context, _ quickbooks.FindCustomerResult) (*dex.StepDecision, error) {
	order, err := paidOrderAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[NewCustomerRequest](createOrderCustomerStepType), NewCustomerRequest{
		DisplayName: order.CustomerDisplayName, EmailAddress: order.CustomerEmail, CompanyName: order.CustomerCompanyName,
	}), nil
}

// dex:group group-id:order group-label:"Paid order"
// dex:explanation text:"Record the customer QuickBooks found and look for an invoice already raised for the order."
type recordFoundCustomer struct {
	dex.StepDefaultsNoWaitFor[quickbooks.FindCustomerResult]
}

func (recordFoundCustomer) GetStepType() string { return recordFoundCustomerStepType }

func (recordFoundCustomer) Execute(ctx dex.Context, result quickbooks.FindCustomerResult) (*dex.StepDecision, error) {
	if result.Value.Customer == nil {
		return dex.ForceFail("QuickBooks reported a customer match without the customer"), nil
	}
	order, err := paidOrderAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	customer := OrderCustomer{Customer: *result.Value.Customer}
	if err := orderCustomerAttribute.Set(ctx, customer); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[OrderInvoiceLookup](findOrderInvoiceStepType),
		OrderInvoiceLookup{CustomerID: customer.Customer.CustomerID, OrderID: order.OrderID}), nil
}

// dex:group group-id:order group-label:"Paid order"
// dex:explanation text:"Record the customer the Flow created and look for an invoice already raised for the order."
type recordCreatedCustomer struct {
	dex.StepDefaultsNoWaitFor[quickbooks.CreateCustomerResult]
}

func (recordCreatedCustomer) GetStepType() string { return recordCreatedCustomerStepType }

func (recordCreatedCustomer) Execute(ctx dex.Context, result quickbooks.CreateCustomerResult) (*dex.StepDecision, error) {
	order, err := paidOrderAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	customer := OrderCustomer{Customer: result.Value.Customer, IsCreated: true}
	if err := orderCustomerAttribute.Set(ctx, customer); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[OrderInvoiceLookup](findOrderInvoiceStepType),
		OrderInvoiceLookup{CustomerID: customer.Customer.CustomerID, OrderID: order.OrderID}), nil
}

// dex:group group-id:order group-label:"Paid order"
// dex:explanation text:"Complete when the order's invoice is already paid, pay an open one, or raise a new invoice."
type chooseOrderInvoice struct {
	dex.StepDefaultsNoWaitFor[quickbooks.ListInvoicesResult]
}

func (chooseOrderInvoice) GetStepType() string { return chooseOrderInvoiceStepType }

func (chooseOrderInvoice) Execute(ctx dex.Context, result quickbooks.ListInvoicesResult) (*dex.StepDecision, error) {
	order, err := paidOrderAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	customer, err := orderCustomerAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	invoice, isFound := ChooseOrderInvoice(result.Value.Invoices, order.OrderID)
	switch {
	case !isFound:
		return dex.GoTo(sdkgo.StepRef[OrderInvoiceRequest](raiseOrderInvoiceStepType),
			OrderInvoiceRequest{CustomerID: customer.Customer.CustomerID, Order: order}), nil
	case !invoice.Balance.IsPositive():
		outcome := PaidOrderOutcome{
			Action: OrderAlreadySettled, OrderID: order.OrderID, CustomerID: customer.Customer.CustomerID,
			IsCustomerCreated: customer.IsCreated, InvoiceID: invoice.InvoiceID, DocNumber: invoice.DocNumber,
			TotalAmount: invoice.TotalAmount, Balance: invoice.Balance, EmailStatus: invoice.EmailStatus, IsFullyPaid: invoice.Balance.IsZero(),
		}
		if err := orderOutcomeAttribute.Set(ctx, outcome); err != nil {
			return nil, err
		}
		return dex.GracefulComplete(outcome), nil
	default:
		return dex.GoTo(sdkgo.StepRef[OrderPayment](recordOrderPaymentStepType), orderPaymentFor(order, invoice)), nil
	}
}

// dex:group group-id:order group-label:"Paid order"
// dex:explanation text:"Pay the exact balance of the invoice QuickBooks raised."
type prepareOrderPayment struct {
	dex.StepDefaultsNoWaitFor[quickbooks.CreateInvoiceResult]
}

func (prepareOrderPayment) GetStepType() string { return prepareOrderPaymentStepType }

func (prepareOrderPayment) Execute(ctx dex.Context, result quickbooks.CreateInvoiceResult) (*dex.StepDecision, error) {
	order, err := paidOrderAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	invoice := result.Value.Invoice
	if !invoice.Balance.IsPositive() {
		return dex.ForceFail("the raised invoice has no balance, so there is no payment to record"), nil
	}
	return dex.GoTo(sdkgo.StepRef[OrderPayment](recordOrderPaymentStepType), orderPaymentFor(order, invoice)), nil
}

func orderPaymentFor(order PaidOrder, invoice quickbooks.Invoice) OrderPayment {
	return OrderPayment{
		CustomerID: invoice.CustomerID, InvoiceID: invoice.InvoiceID, Amount: invoice.Balance, PaidOn: order.PaidOn,
		DepositAccountID: order.DepositAccountID, Reference: order.PaymentReference,
	}
}

// dex:group group-id:order group-label:"Paid order"
// dex:explanation text:"Address the paid invoice to the customer's email as their receipt."
type prepareInvoiceDelivery struct {
	dex.StepDefaultsNoWaitFor[quickbooks.RecordPaymentResult]
}

func (prepareInvoiceDelivery) GetStepType() string { return prepareInvoiceDeliveryStepType }

func (prepareInvoiceDelivery) Execute(ctx dex.Context, result quickbooks.RecordPaymentResult) (*dex.StepDecision, error) {
	order, err := paidOrderAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if len(result.Value.AppliedInvoices) != 1 {
		return dex.ForceFail("the recorded payment does not apply to exactly one invoice"), nil
	}
	return dex.GoTo(sdkgo.StepRef[InvoiceDelivery](sendPaidInvoiceStepType),
		InvoiceDelivery{InvoiceID: result.Value.AppliedInvoices[0].InvoiceID, SendTo: order.CustomerEmail}), nil
}

// dex:group group-id:order group-label:"Paid order"
// dex:explanation text:"Record what the Flow wrote and whether the read-back invoice is fully paid, then complete."
type completePaidOrder struct {
	dex.StepDefaultsNoWaitFor[quickbooks.GetInvoiceResult]
}

func (completePaidOrder) GetStepType() string { return completePaidOrderStepType }

func (completePaidOrder) Execute(ctx dex.Context, result quickbooks.GetInvoiceResult) (*dex.StepDecision, error) {
	order, err := paidOrderAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	customer, err := orderCustomerAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	payment, err := recordedPaymentAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	// RaiseOrderInvoice writes its Result only when this Flow raised the invoice.
	raised, err := raisedInvoiceAttribute.Get(ctx)
	var missingRaisedInvoice *dex.AttributeNotFoundError
	if err != nil && !errors.As(err, &missingRaisedInvoice) {
		return nil, err
	}
	action := OrderExistingInvoicePaid
	if raised.Value.Invoice.InvoiceID != "" {
		action = OrderInvoicedAndPaid
	}
	invoice := result.Value
	outcome := PaidOrderOutcome{
		Action: action, OrderID: order.OrderID, CustomerID: customer.Customer.CustomerID, IsCustomerCreated: customer.IsCreated,
		InvoiceID: invoice.InvoiceID, DocNumber: invoice.DocNumber, TotalAmount: invoice.TotalAmount, Balance: invoice.Balance,
		EmailStatus: invoice.EmailStatus, PaymentID: payment.Value.PaymentID, IsFullyPaid: invoice.Balance.IsZero(),
	}
	if err := orderOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// ChooseOrderInvoice returns the listed invoice whose number is the order ID, ignoring case as
// QuickBooks does, preferring a paid one, or false when there is none.
func ChooseOrderInvoice(invoices []quickbooks.Invoice, orderID string) (quickbooks.Invoice, bool) {
	var chosen quickbooks.Invoice
	isFound := false
	for _, invoice := range invoices {
		if !strings.EqualFold(invoice.DocNumber, orderID) {
			continue
		}
		if !isFound || invoice.Balance.IsZero() {
			chosen, isFound = invoice, true
		}
	}
	return chosen, isFound
}

// BuildPaidOrder validates Start Flow input so no connector Step receives an unusable order.
// Prices are checked again, exactly, by the connector before any request.
func BuildPaidOrder(input Input) (PaidOrder, error) {
	order := PaidOrder(input)
	order.OrderID, order.CustomerEmail = strings.TrimSpace(input.OrderID), strings.TrimSpace(input.CustomerEmail)
	order.CustomerDisplayName, order.CustomerCompanyName = strings.TrimSpace(input.CustomerDisplayName), strings.TrimSpace(input.CustomerCompanyName)
	order.InvoiceDate, order.DueDate, order.PaidOn = strings.TrimSpace(input.InvoiceDate), strings.TrimSpace(input.DueDate), strings.TrimSpace(input.PaidOn)
	order.DepositAccountID, order.PaymentReference = strings.TrimSpace(input.DepositAccountID), strings.TrimSpace(input.PaymentReference)
	if !orderIDPattern.MatchString(order.OrderID) {
		return PaidOrder{}, errors.New("orderId must be 1 to 21 letters, digits, or . _ / - such as ORDER-1042, because it becomes the invoice number")
	}
	address, err := mail.ParseAddress(order.CustomerEmail)
	if err != nil || address.Name != "" || address.Address != order.CustomerEmail {
		return PaidOrder{}, errors.New("customerEmail must be one bare email address such as accounts@example.com")
	}
	if order.CustomerDisplayName == "" || strings.Contains(order.CustomerDisplayName, ":") {
		return PaidOrder{}, errors.New("customerDisplayName is required and cannot contain a colon")
	}
	for _, date := range []struct{ name, value string }{{"invoiceDate", order.InvoiceDate}, {"dueDate", order.DueDate}, {"paidOn", order.PaidOn}} {
		if _, err := time.Parse("2006-01-02", date.value); err != nil {
			return PaidOrder{}, fmt.Errorf("%s must be a date written YYYY-MM-DD", date.name)
		}
	}
	if len(order.Lines) == 0 || len(order.Lines) > maximumOrderLines {
		return PaidOrder{}, fmt.Errorf("lines must hold 1 to %d order lines", maximumOrderLines)
	}
	order.Lines = make([]OrderLine, 0, len(input.Lines))
	for index, line := range input.Lines {
		line.ItemID, line.Description, line.TaxCodeID = strings.TrimSpace(line.ItemID), strings.TrimSpace(line.Description), strings.TrimSpace(line.TaxCodeID)
		if line.ItemID == "" || line.Quantity == "" || line.UnitPrice == "" {
			return PaidOrder{}, fmt.Errorf("lines[%d] needs an itemId, quantity, and unitPrice", index)
		}
		order.Lines = append(order.Lines, line)
	}
	return order, nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
