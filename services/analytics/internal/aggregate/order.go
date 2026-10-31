// Package aggregate maintains every precomputed aggregate table from order events.
//
// The worker (live ingestion) and the rebuild handler (replay from raw_events)
// both call ProcessOrderEvent, so the two paths cannot drift apart.
//
// The method is the same for all tables. An order has a *contribution* to each
// table, computed purely from its current state. When an event changes an
// order, the engine books  contribution(new) - contribution(old)  into every
// table. Cancellations, re-activations, and changes to any dimension (city,
// payment status, channel, ...) all fall out of that single rule.
package aggregate

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// SchemaV2 is the first event schema version that carries the extended order fields.
const SchemaV2 = 2

// Order is the normalized state of one order.
type Order struct {
	OrderID     string
	AmountC     int64 // cents
	Currency    string
	Status      string
	ProductID   string
	ProductName string
	Category    string

	// Extended is false for orders that only ever arrived in schema v1. The
	// fields below hold neutral defaults for such orders and are ignored by every
	// extended contribution, so defaults can never be mistaken for real data.
	Extended bool

	CustomerID        string
	CustomerTier      string
	PaymentMethod     string
	PaymentStatus     string
	DiscountCode      string
	DiscountC         int64 // cents
	TaxC              int64 // cents
	ShippingC         int64 // cents
	Quantity          int64
	SKU               string
	FulfillmentStatus string
	ReturnReason      string
	Country           string
	StateProvince     string
	City              string
	Channel           string
	ReferrerSource    string

	// IsNewCustomer is decided once, when the order is first seen: the customer
	// had no earlier order for this tenant.
	IsNewCustomer bool

	// CreatedAt is the time of the order's first event (order_state.occurred_at).
	// Cohort-style tables are bucketed by this, never by the time of a later update.
	CreatedAt time.Time
}

func (o *Order) cancelled() bool { return o.Status == "cancelled" }

// returned reports whether the order was returned by the customer.
func (o *Order) returned() bool { return o.Status == "returned" || o.FulfillmentStatus == "returned" }

// Defaults for extended fields that are absent from a v2 event (schema plan §1).
const (
	defTier          = "regular"
	defPaymentMethod = "card"
	defPaymentStatus = "paid"
	defFulfillment   = "pending"
	defCountry       = "IN"
	defChannel       = "web"
	unknownCountry   = "ZZ" // CLDR "unknown region"; used when a country value is not a 2-letter code
)

// Column widths in order_state. Values are truncated to fit so that one
// oversized field cannot poison an event into the dead-letter queue.
const (
	wID       = 64
	wName     = 255
	wCategory = 64
	wShort    = 32
	wCode     = 64
	wReason   = 128
	wPlace    = 64
)

// FromEvent builds an Order from an event's data payload. ok is false when the
// event carries no order id (not an order event).
func FromEvent(data map[string]interface{}, schemaVersion int) (o Order, ok bool) {
	o.OrderID = text(data, wID, "", "order_id", "id")
	if o.OrderID == "" {
		return o, false
	}
	o.AmountC = cents(data, "amount")
	o.Currency = strings.ToUpper(text(data, 3, "INR", "currency"))
	o.Status = text(data, wShort, "placed", "status")
	o.ProductID = text(data, wID, "", "product_id")
	o.ProductName = text(data, wName, "Unknown Product", "product_name")
	o.Category = text(data, wCategory, "General", "category")

	o.Extended = schemaVersion >= SchemaV2

	o.CustomerID = text(data, wID, "", "customer_id")
	o.CustomerTier = lower(text(data, wShort, defTier, "customer_tier"))
	o.PaymentMethod = lower(text(data, wShort, defPaymentMethod, "payment_method"))
	o.PaymentStatus = lower(text(data, wShort, defPaymentStatus, "payment_status"))
	o.DiscountCode = text(data, wCode, "", "discount_code")
	o.DiscountC = cents(data, "discount_amount")
	o.TaxC = cents(data, "tax_amount")
	o.ShippingC = cents(data, "shipping_cost")
	o.Quantity = quantity(data, "quantity")
	o.SKU = text(data, wCode, "", "sku")
	o.ReturnReason = lower(text(data, wReason, "", "return_reason"))
	o.StateProvince = text(data, wPlace, "", "state_province")
	o.City = text(data, wPlace, "", "city")
	o.Channel = lower(text(data, wShort, defChannel, "channel"))
	o.ReferrerSource = lower(text(data, wShort, "", "referrer_source"))
	o.Country = country(data)

	// fulfillment_status follows the order status when an extended event does not
	// say. A v1 order has no fulfilment stage; it keeps the column default so a
	// migrated row and a replayed row hold identical data.
	o.FulfillmentStatus = lower(text(data, wShort, "", "fulfillment_status"))
	if !o.Extended {
		o.FulfillmentStatus = defFulfillment
	} else if o.FulfillmentStatus == "" {
		o.FulfillmentStatus = fulfillmentFor(o.Status)
	}
	return o, true
}

// fulfillmentFor derives a fulfilment stage from an order status.
func fulfillmentFor(status string) string {
	switch status {
	case "confirmed":
		return "processing"
	case "shipped", "delivered", "cancelled", "returned":
		return status
	default: // placed and anything unknown
		return "pending"
	}
}

// carryExtended copies the extended fields (and flag) from a previous state.
// A v1 event cannot erase information it simply does not carry.
func (o *Order) carryExtended(from *Order) {
	o.Extended = true
	o.CustomerID, o.CustomerTier = from.CustomerID, from.CustomerTier
	o.PaymentMethod, o.PaymentStatus = from.PaymentMethod, from.PaymentStatus
	o.DiscountCode, o.DiscountC, o.TaxC, o.ShippingC = from.DiscountCode, from.DiscountC, from.TaxC, from.ShippingC
	o.Quantity, o.SKU = from.Quantity, from.SKU
	o.FulfillmentStatus, o.ReturnReason = from.FulfillmentStatus, from.ReturnReason
	o.Country, o.StateProvince, o.City = from.Country, from.StateProvince, from.City
	o.Channel, o.ReferrerSource = from.Channel, from.ReferrerSource
}

// ─── Field parsing ───────────────────────────────────────────────────────────

// raw returns the first present, non-nil value among keys.
func raw(m map[string]interface{}, keys ...string) (interface{}, bool) {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return v, true
		}
	}
	return nil, false
}

// text returns the first present key as a trimmed string, truncated to max
// characters, or def when absent or blank.
func text(m map[string]interface{}, max int, def string, keys ...string) string {
	v, ok := raw(m, keys...)
	if !ok {
		return truncate(def, max)
	}
	var s string
	switch t := v.(type) {
	case string:
		s = t
	case float64:
		s = strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		s = strconv.FormatBool(t)
	default:
		s = fmt.Sprintf("%v", t)
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return truncate(def, max)
	}
	return truncate(s, max)
}

func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max])
}

func lower(s string) string { return strings.ToLower(s) }

func number(v interface{}) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f, err == nil
	}
	return 0, false
}

// cents converts a decimal amount to integer cents, avoiding float drift in sums.
func cents(m map[string]interface{}, key string) int64 {
	v, ok := raw(m, key)
	if !ok {
		return 0
	}
	f, ok := number(v)
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return int64(math.Round(f * 100))
}

// quantity defaults to 1 and is clamped to a sane positive range.
func quantity(m map[string]interface{}, key string) int64 {
	v, ok := raw(m, key)
	if !ok {
		return 1
	}
	f, ok := number(v)
	if !ok || math.IsNaN(f) {
		return 1
	}
	q := int64(math.Round(f))
	if q < 1 {
		return 1
	}
	if q > 1_000_000 {
		return 1_000_000
	}
	return q
}

// country normalizes to an upper-case ISO 3166-1 alpha-2 code. A missing value
// takes the schema default; a present but malformed one maps to "ZZ".
func country(m map[string]interface{}) string {
	s := strings.ToUpper(text(m, 64, "", "country"))
	if s == "" {
		return defCountry
	}
	if len(s) == 2 && s[0] >= 'A' && s[0] <= 'Z' && s[1] >= 'A' && s[1] <= 'Z' {
		return s
	}
	return unknownCountry
}
