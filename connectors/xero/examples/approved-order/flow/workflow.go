// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package approvedorder demonstrates the Xero connector in one Flow started from Dex Web Start
// Flow: for an approved and paid order, find the customer's Xero contact by email, look for an
// invoice already raised for the order, raise an approved sales invoice with exact decimal line
// amounts when there is none, record the order's payment against it, and read the invoice back
// to confirm it is paid.
package approvedorder

import (
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/xero"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "XeroApprovedOrderInvoice"
	// ConnectionName is the static Dex Web connection for Xero.
	ConnectionName = "xero-books"

	recordApprovedOrderStepType       = "RecordApprovedOrder"
	findCustomerContactStepType       = "FindCustomerContact"
	prepareOrderInvoiceLookupStepType = "PrepareOrderInvoiceLookup"
	findOrderInvoiceStepType          = "FindOrderInvoice"
	chooseOrderInvoiceStepType        = "ChooseOrderInvoice"
	raiseOrderInvoiceStepType         = "RaiseOrderInvoice"
	prepareOrderPaymentStepType       = "PrepareOrderPayment"
	recordOrderPaymentStepType        = "RecordOrderPayment"
	readBackPaidInvoiceStepType       = "ReadBackPaidInvoice"
	completeInvoicedOrderStepType     = "CompleteInvoicedOrder"
	completeWithoutContactStepType    = "CompleteWithoutContact"

	// orderInvoiceSearchPageSize bounds the invoices read for one order reference.
	orderInvoiceSearchPageSize = 10
	maximumOrderLines          = 50
)

var (
	approvedOrderAttribute   = dex.DefineAttribute[ApprovedOrder]("xero-approved-order")
	customerContactAttribute = dex.DefineAttribute[xero.Contact]("xero-order-customer")
	raisedInvoiceAttribute   = dex.DefineAttribute[sdkgo.MutationResult[xero.CreateInvoiceOutput]]("xero-order-raised-invoice")
	recordedPaymentAttribute = dex.DefineAttribute[sdkgo.MutationResult[xero.RecordPaymentOutput]]("xero-order-recorded-payment")
	orderOutcomeAttribute    = dex.DefineAttribute[InvoicedOrderOutcome]("xero-invoiced-order-outcome")

	orderIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,63}$`)
)

// Input is the approved, paid order entered in Dex Web Start Flow.
type Input struct {
	// OrderID is the application's order ID, such as ORDER-1042; it becomes the invoice Reference.
	OrderID string `json:"orderId"`
	// CustomerEmail is the email address of the customer's existing Xero contact.
	CustomerEmail string `json:"customerEmail"`
	// CurrencyCode is the ISO 4217 currency of the order, enabled in the Xero organisation.
	CurrencyCode string `json:"currencyCode"`
	// InvoiceDate is the invoice issue date, YYYY-MM-DD.
	InvoiceDate string `json:"invoiceDate"`
	// DueDate is the invoice due date, YYYY-MM-DD.
	DueDate string `json:"dueDate"`
	// Lines are the order lines, with amounts as exact decimal strings excluding tax.
	Lines []OrderLine `json:"lines"`
	// PaidOn is the date the customer paid, YYYY-MM-DD.
	PaidOn string `json:"paidOn"`
	// PaymentAccountCode is the code of the Xero bank account that received the money, such as 090.
	PaymentAccountCode string `json:"paymentAccountCode"`
	// PaymentReference is an optional payment description, such as the card charge ID.
	PaymentReference string `json:"paymentReference,omitempty"`
}

// OrderLine is one order line.
type OrderLine struct {
	// Description is the line description shown on the invoice.
	Description string `json:"description"`
	// Quantity is the quantity, an exact decimal string such as 2.
	Quantity xero.Decimal `json:"quantity"`
	// UnitAmount is the unit price excluding tax, an exact decimal string such as 49.95.
	UnitAmount xero.Decimal `json:"unitAmount"`
	// AccountCode is the Xero sales account code, such as 200.
	AccountCode string `json:"accountCode"`
	// TaxType optionally overrides the account's Xero tax type, such as OUTPUT or NONE.
	TaxType string `json:"taxType,omitempty"`
}

// ApprovedOrder is the validated order every later Step reads.
type ApprovedOrder Input

// OrderInvoiceLookup asks Xero for invoices already raised for the order.
type OrderInvoiceLookup struct {
	// ContactID is the customer's Xero contact.
	ContactID string `json:"contactId"`
	// OrderID is the order reference to look for.
	OrderID string `json:"orderId"`
}

// OrderInvoiceRequest is the invoice to raise for the order.
type OrderInvoiceRequest struct {
	// ContactID is the customer's Xero contact.
	ContactID string `json:"contactId"`
	// Order is the validated order.
	Order ApprovedOrder `json:"order"`
}

// OrderPayment is the payment to record against the order's invoice.
type OrderPayment struct {
	// InvoiceID is the invoice to pay.
	InvoiceID string `json:"invoiceId"`
	// Amount is the exact amount still due on the invoice.
	Amount xero.Decimal `json:"amount"`
	// PaidOn is the payment date.
	PaidOn string `json:"paidOn"`
	// AccountCode is the receiving bank account's code.
	AccountCode string `json:"accountCode"`
	// Reference is the optional payment description.
	Reference string `json:"reference,omitempty"`
}

// InvoicedOrderAction is the Flow's terminal business outcome.
type InvoicedOrderAction string

const (
	// OrderInvoicedAndPaid means the Flow raised the invoice and recorded the payment.
	OrderInvoicedAndPaid InvoicedOrderAction = "invoicedAndPaid"
	// OrderExistingInvoicePaid means an approved invoice for the order existed and the Flow recorded the payment.
	OrderExistingInvoicePaid InvoicedOrderAction = "existingInvoicePaid"
	// OrderAlreadySettled means a paid invoice for the order existed, so the Flow wrote nothing.
	OrderAlreadySettled InvoicedOrderAction = "alreadySettled"
	// OrderContactMissing means no Xero contact has the customer's email, so the Flow wrote nothing.
	OrderContactMissing InvoicedOrderAction = "contactMissing"
)

// InvoicedOrderOutcome is the Flow result and the value of its outcome Attribute.
type InvoicedOrderOutcome struct {
	// Action is what the Flow did.
	Action InvoicedOrderAction `json:"action"`
	// OrderID is the order.
	OrderID string `json:"orderId"`
	// ContactID is the customer's Xero contact, when found.
	ContactID string `json:"contactId,omitempty"`
	// InvoiceID is the order's invoice, when one exists.
	InvoiceID string `json:"invoiceId,omitempty"`
	// InvoiceNumber is the document number Xero assigned.
	InvoiceNumber string `json:"invoiceNumber,omitempty"`
	// InvoiceStatus is the invoice's status as last read.
	InvoiceStatus xero.InvoiceStatus `json:"invoiceStatus,omitempty"`
	// CurrencyCode is the invoice's currency.
	CurrencyCode string `json:"currencyCode,omitempty"`
	// Total is the invoice total, exactly as Xero stores it.
	Total xero.Decimal `json:"total,omitempty"`
	// AmountDue is the amount still due as last read.
	AmountDue xero.Decimal `json:"amountDue,omitempty"`
	// PaymentID is the payment the Flow recorded.
	PaymentID string `json:"paymentId,omitempty"`
	// IsFullyPaid reports that the read-back invoice is PAID with nothing due.
	IsFullyPaid bool `json:"isFullyPaid"`
}

// Flow invoices one approved order in Xero and records its payment.
type Flow struct {
	dex.FlowDefaults
	connection xero.Connection
}

// NewFlow binds the Xero Connection at registration time.
func NewFlow(connection xero.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Xero connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordApprovedOrder{}),
		dex.DefineStep(xero.NewFindContactByEmailStep(xero.FindContactByEmailStepConfig[ApprovedOrder]{
			StepType: findCustomerContactStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "xero", GroupLabel: "Xero",
				Explanation: "Find the customer's Xero contact by the order's email address.",
			},
			Connection: flow.connection, MapToOperationInput: MapToFindContactByEmailInput,
			Found:    sdkgo.GoTo(prepareOrderInvoiceLookup{}),
			NotFound: sdkgo.GoTo(completeWithoutContact{}),
		})),
		dex.DefineStep(prepareOrderInvoiceLookup{}),
		dex.DefineStep(xero.NewListInvoicesStep(xero.ListInvoicesStepConfig[OrderInvoiceLookup]{
			StepType: findOrderInvoiceStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "xero", GroupLabel: "Xero",
				Explanation: "List the customer's approved or paid sales invoices whose reference is the order ID.",
			},
			Connection: flow.connection, MapToOperationInput: MapToListInvoicesInput,
			Listed: sdkgo.GoTo(chooseOrderInvoice{}),
		})),
		dex.DefineStep(chooseOrderInvoice{}),
		dex.DefineStep(xero.NewCreateInvoiceStep(xero.CreateInvoiceStepConfig[OrderInvoiceRequest]{
			StepType: raiseOrderInvoiceStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "xero", GroupLabel: "Xero",
				Explanation: "Raise an approved sales invoice for the order under a Step-derived Idempotency-Key, so a retry returns the same invoice.",
			},
			Connection: flow.connection, MapToOperationInput: MapToCreateInvoiceInput,
			ResultAttribute: &raisedInvoiceAttribute,
			Created:         sdkgo.GoTo(prepareOrderPayment{}),
		})),
		dex.DefineStep(prepareOrderPayment{}),
		dex.DefineStep(xero.NewRecordPaymentStep(xero.RecordPaymentStepConfig[OrderPayment]{
			StepType: recordOrderPaymentStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "xero", GroupLabel: "Xero",
				Explanation: "Record the order's payment against the invoice under a Step-derived Idempotency-Key, so a retry never pays twice.",
			},
			Connection: flow.connection, MapToOperationInput: MapToRecordPaymentInput,
			ResultAttribute: &recordedPaymentAttribute,
			Recorded:        sdkgo.GoTo(sdkgo.StepRef[xero.RecordPaymentResult](readBackPaidInvoiceStepType)),
		})),
		dex.DefineStep(xero.NewGetInvoiceStep(xero.GetInvoiceStepConfig[xero.RecordPaymentResult]{
			StepType: readBackPaidInvoiceStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "xero", GroupLabel: "Xero",
				Explanation: "Read the invoice back to confirm the payment landed and nothing is due.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetInvoiceInput,
			Found: sdkgo.GoTo(completeInvoicedOrder{}),
		})),
		dex.DefineStep(completeInvoicedOrder{}),
		dex.DefineStep(completeWithoutContact{}),
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
		approvedOrderAttribute, customerContactAttribute, raisedInvoiceAttribute, recordedPaymentAttribute, orderOutcomeAttribute,
	}}
}

// GetDexSummary returns the order and its outcome.
//
// dex:field attribute-key:xero-approved-order value-type:json editable:false description:"Approved order"
// dex:field attribute-key:xero-invoiced-order-outcome value-type:json editable:false description:"Xero outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	order, outcome, err := orderInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"xero-approved-order": order, "xero-invoiced-order-outcome": outcome}}, nil
}

// GetDexDisplay returns the order and its invoice and payment outcome.
//
// dex:field attribute-key:xero-approved-order value-type:json editable:false description:"Order, customer email, lines, and payment"
// dex:field attribute-key:xero-invoiced-order-outcome value-type:json editable:false description:"Action, invoice, and payment"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	order, outcome, err := orderInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"xero-approved-order": order, "xero-invoiced-order-outcome": outcome}}, nil
}

// MapToFindContactByEmailInput looks up the customer's active contact.
func MapToFindContactByEmailInput(order ApprovedOrder) xero.FindContactByEmailInput {
	return xero.FindContactByEmailInput{EmailAddress: order.CustomerEmail}
}

// MapToListInvoicesInput finds approved or paid sales invoices for the customer with the order's reference.
func MapToListInvoicesInput(lookup OrderInvoiceLookup) xero.ListInvoicesInput {
	return xero.ListInvoicesInput{
		Type: xero.InvoiceTypeAccountsReceivable, ContactIDs: []string{lookup.ContactID}, Reference: lookup.OrderID,
		Statuses: []xero.InvoiceStatus{xero.InvoiceStatusAuthorised, xero.InvoiceStatusPaid}, PageSize: orderInvoiceSearchPageSize,
	}
}

// MapToCreateInvoiceInput raises an approved sales invoice whose reference is the order ID; Xero numbers it.
func MapToCreateInvoiceInput(request OrderInvoiceRequest) xero.CreateInvoiceInput {
	order := request.Order
	lines := make([]xero.InvoiceLineItemInput, 0, len(order.Lines))
	for _, line := range order.Lines {
		lines = append(lines, xero.InvoiceLineItemInput{
			Description: line.Description, Quantity: line.Quantity, UnitAmount: line.UnitAmount,
			AccountCode: line.AccountCode, TaxType: line.TaxType,
		})
	}
	return xero.CreateInvoiceInput{
		Type: xero.InvoiceTypeAccountsReceivable, ContactID: request.ContactID, LineItems: lines,
		Date: order.InvoiceDate, DueDate: order.DueDate, LineAmountTypes: xero.LineAmountTypesExclusive,
		CurrencyCode: order.CurrencyCode, Reference: order.OrderID, Status: xero.InvoiceStatusAuthorised,
	}
}

// MapToRecordPaymentInput pays the exact amount due from the receiving bank account.
func MapToRecordPaymentInput(payment OrderPayment) xero.RecordPaymentInput {
	return xero.RecordPaymentInput{
		InvoiceID: payment.InvoiceID, AccountCode: payment.AccountCode, Date: payment.PaidOn,
		Amount: payment.Amount, Reference: payment.Reference,
	}
}

// MapToGetInvoiceInput reads back the invoice the payment applied to.
func MapToGetInvoiceInput(result xero.RecordPaymentResult) xero.GetInvoiceInput {
	return xero.GetInvoiceInput{InvoiceID: result.Value.InvoiceID}
}

// ChooseOrderInvoice returns the invoice the listing found for the order's exact reference,
// preferring a paid one, or false when there is none.
func ChooseOrderInvoice(invoices []xero.Invoice, orderID string) (xero.Invoice, bool) {
	var chosen xero.Invoice
	isFound := false
	for _, invoice := range invoices {
		if invoice.Reference != orderID || (invoice.Status != xero.InvoiceStatusAuthorised && invoice.Status != xero.InvoiceStatusPaid) {
			continue
		}
		if !isFound || invoice.Status == xero.InvoiceStatusPaid {
			chosen, isFound = invoice, true
		}
	}
	return chosen, isFound
}

// BuildApprovedOrder validates Start Flow input so no connector Step receives an unusable order.
// Amounts are checked again, exactly, by the connector before any request.
func BuildApprovedOrder(input Input) (ApprovedOrder, error) {
	order := ApprovedOrder(input)
	order.OrderID, order.CustomerEmail = strings.TrimSpace(input.OrderID), strings.TrimSpace(input.CustomerEmail)
	order.CurrencyCode, order.PaymentAccountCode = strings.TrimSpace(input.CurrencyCode), strings.TrimSpace(input.PaymentAccountCode)
	order.InvoiceDate, order.DueDate, order.PaidOn = strings.TrimSpace(input.InvoiceDate), strings.TrimSpace(input.DueDate), strings.TrimSpace(input.PaidOn)
	order.PaymentReference = strings.TrimSpace(input.PaymentReference)
	if !orderIDPattern.MatchString(order.OrderID) {
		return ApprovedOrder{}, errors.New("orderId must be 1 to 64 letters, digits, or . _ / - such as ORDER-1042")
	}
	address, err := mail.ParseAddress(order.CustomerEmail)
	if err != nil || address.Name != "" || address.Address != order.CustomerEmail {
		return ApprovedOrder{}, fmt.Errorf("customerEmail %q must be one bare email address", input.CustomerEmail)
	}
	if order.CurrencyCode == "" || order.PaymentAccountCode == "" {
		return ApprovedOrder{}, errors.New("currencyCode and paymentAccountCode are required")
	}
	for _, date := range []struct{ name, value string }{{"invoiceDate", order.InvoiceDate}, {"dueDate", order.DueDate}, {"paidOn", order.PaidOn}} {
		if _, err := time.Parse("2006-01-02", date.value); err != nil {
			return ApprovedOrder{}, fmt.Errorf("%s must be a date written YYYY-MM-DD", date.name)
		}
	}
	if len(order.Lines) == 0 || len(order.Lines) > maximumOrderLines {
		return ApprovedOrder{}, fmt.Errorf("lines must hold 1 to %d order lines", maximumOrderLines)
	}
	order.Lines = make([]OrderLine, 0, len(input.Lines))
	for index, line := range input.Lines {
		line.Description, line.AccountCode, line.TaxType = strings.TrimSpace(line.Description), strings.TrimSpace(line.AccountCode), strings.TrimSpace(line.TaxType)
		if line.Description == "" || line.AccountCode == "" || line.Quantity == "" || line.UnitAmount == "" {
			return ApprovedOrder{}, fmt.Errorf("lines[%d] needs a description, quantity, unitAmount, and accountCode", index)
		}
		order.Lines = append(order.Lines, line)
	}
	return order, nil
}

func orderInspection(ctx dex.Context) (ApprovedOrder, InvoicedOrderOutcome, error) {
	order, err := optionalAttribute(ctx, approvedOrderAttribute)
	if err != nil {
		return ApprovedOrder{}, InvoicedOrderOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, orderOutcomeAttribute)
	if err != nil {
		return ApprovedOrder{}, InvoicedOrderOutcome{}, err
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

// dex:group group-id:order group-label:"Approved order"
// dex:explanation text:"Validate the approved order and record it before calling Xero."
type recordApprovedOrder struct {
	dex.StepDefaults
}

func (recordApprovedOrder) GetStepType() string { return recordApprovedOrderStepType }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordApprovedOrder) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordApprovedOrder) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	order, err := BuildApprovedOrder(input)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := approvedOrderAttribute.Set(ctx, order); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[ApprovedOrder](findCustomerContactStepType), order), nil
}

// dex:group group-id:order group-label:"Approved order"
// dex:explanation text:"Record the customer's contact and look for an invoice already raised for the order."
type prepareOrderInvoiceLookup struct {
	dex.StepDefaultsNoWaitFor[xero.FindContactByEmailResult]
}

func (prepareOrderInvoiceLookup) GetStepType() string { return prepareOrderInvoiceLookupStepType }

func (prepareOrderInvoiceLookup) Execute(ctx dex.Context, result xero.FindContactByEmailResult) (*dex.StepDecision, error) {
	order, err := approvedOrderAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if result.Value.Contact == nil {
		return dex.ForceFail("Xero reported a contact match without the contact"), nil
	}
	if err := customerContactAttribute.Set(ctx, *result.Value.Contact); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[OrderInvoiceLookup](findOrderInvoiceStepType),
		OrderInvoiceLookup{ContactID: result.Value.Contact.ContactID, OrderID: order.OrderID}), nil
}

// dex:group group-id:order group-label:"Approved order"
// dex:explanation text:"Complete when the order is already paid in Xero, pay an approved invoice for it, or raise a new invoice."
type chooseOrderInvoice struct {
	dex.StepDefaultsNoWaitFor[xero.ListInvoicesResult]
}

func (chooseOrderInvoice) GetStepType() string { return chooseOrderInvoiceStepType }

func (chooseOrderInvoice) Execute(ctx dex.Context, result xero.ListInvoicesResult) (*dex.StepDecision, error) {
	order, err := approvedOrderAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	contact, err := customerContactAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	invoice, isFound := ChooseOrderInvoice(result.Value.Invoices, order.OrderID)
	switch {
	case !isFound:
		return dex.GoTo(sdkgo.StepRef[OrderInvoiceRequest](raiseOrderInvoiceStepType),
			OrderInvoiceRequest{ContactID: contact.ContactID, Order: order}), nil
	case invoice.Status == xero.InvoiceStatusPaid || invoice.AmountDue.IsZero():
		outcome := InvoicedOrderOutcome{
			Action: OrderAlreadySettled, OrderID: order.OrderID, ContactID: contact.ContactID, InvoiceID: invoice.InvoiceID,
			InvoiceNumber: invoice.InvoiceNumber, InvoiceStatus: invoice.Status, CurrencyCode: invoice.CurrencyCode,
			Total: invoice.Total, AmountDue: invoice.AmountDue, IsFullyPaid: invoice.Status == xero.InvoiceStatusPaid,
		}
		if err := orderOutcomeAttribute.Set(ctx, outcome); err != nil {
			return nil, err
		}
		return dex.GracefulComplete(outcome), nil
	default:
		return dex.GoTo(sdkgo.StepRef[OrderPayment](recordOrderPaymentStepType), orderPaymentFor(order, invoice)), nil
	}
}

// dex:group group-id:order group-label:"Approved order"
// dex:explanation text:"Pay the exact amount due on the invoice Xero raised."
type prepareOrderPayment struct {
	dex.StepDefaultsNoWaitFor[xero.CreateInvoiceResult]
}

func (prepareOrderPayment) GetStepType() string { return prepareOrderPaymentStepType }

func (prepareOrderPayment) Execute(ctx dex.Context, result xero.CreateInvoiceResult) (*dex.StepDecision, error) {
	order, err := approvedOrderAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	invoice := result.Value.Invoice
	if invoice.AmountDue == "" || invoice.AmountDue.IsZero() || invoice.AmountDue.IsNegative() {
		return dex.ForceFail("the raised invoice has no amount due, so there is no payment to record"), nil
	}
	return dex.GoTo(sdkgo.StepRef[OrderPayment](recordOrderPaymentStepType), orderPaymentFor(order, invoice)), nil
}

func orderPaymentFor(order ApprovedOrder, invoice xero.Invoice) OrderPayment {
	return OrderPayment{
		InvoiceID: invoice.InvoiceID, Amount: invoice.AmountDue, PaidOn: order.PaidOn,
		AccountCode: order.PaymentAccountCode, Reference: order.PaymentReference,
	}
}

// dex:group group-id:order group-label:"Approved order"
// dex:explanation text:"Record what the Flow wrote and whether the read-back invoice is fully paid, then complete."
type completeInvoicedOrder struct {
	dex.StepDefaultsNoWaitFor[xero.GetInvoiceResult]
}

func (completeInvoicedOrder) GetStepType() string { return completeInvoicedOrderStepType }

func (completeInvoicedOrder) Execute(ctx dex.Context, result xero.GetInvoiceResult) (*dex.StepDecision, error) {
	order, err := approvedOrderAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	contact, err := customerContactAttribute.Get(ctx)
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
	outcome := InvoicedOrderOutcome{
		Action: action, OrderID: order.OrderID, ContactID: contact.ContactID, InvoiceID: invoice.InvoiceID,
		InvoiceNumber: invoice.InvoiceNumber, InvoiceStatus: invoice.Status, CurrencyCode: invoice.CurrencyCode,
		Total: invoice.Total, AmountDue: invoice.AmountDue, PaymentID: payment.Value.Payment.PaymentID,
		IsFullyPaid: invoice.Status == xero.InvoiceStatusPaid && invoice.AmountDue.IsZero(),
	}
	if err := orderOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:order group-label:"Approved order"
// dex:explanation text:"Complete without writing when no Xero contact has the customer's email."
type completeWithoutContact struct {
	dex.StepDefaultsNoWaitFor[xero.FindContactByEmailResult]
}

func (completeWithoutContact) GetStepType() string { return completeWithoutContactStepType }

func (completeWithoutContact) Execute(ctx dex.Context, _ xero.FindContactByEmailResult) (*dex.StepDecision, error) {
	order, err := approvedOrderAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := InvoicedOrderOutcome{Action: OrderContactMissing, OrderID: order.OrderID}
	if err := orderOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
