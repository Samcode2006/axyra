package aggregate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Tables that hold per-tenant derived data. Rebuild clears exactly these before
// replaying raw events, so this list must contain every table Apply writes to.
var TenantTables = []string{
	"order_summary_hourly",
	"order_summary_daily",
	"product_aggregates",
	"customer_aggregates",
	"geo_aggregates",
	"channel_aggregates",
	"payment_aggregates",
	"return_aggregates",
	"fulfillment_aggregates",
	"cohort_daily",
	"discount_aggregates",
	"order_state",
}

const (
	customerNew       = "new"
	customerReturning = "returning"
)

// ProcessOrderEvent applies one order event to order_state and every aggregate
// table inside tx. eventAt is the event's occurred_at. It is a no-op for events
// that carry no order id. Both the live worker and the rebuild call this.
func ProcessOrderEvent(ctx context.Context, tx *sql.Tx, tenantID string, eventAt time.Time,
	data map[string]interface{}, schemaVersion int) error {

	next, ok := FromEvent(data, schemaVersion)
	if !ok {
		return nil
	}
	eventAt = eventAt.UTC()

	prior, exists, err := loadState(ctx, tx, tenantID, next.OrderID)
	if err != nil {
		return err
	}

	if exists {
		next.CreatedAt = prior.CreatedAt
		next.IsNewCustomer = prior.IsNewCustomer
		if !next.Extended && prior.Extended {
			next.carryExtended(prior)
		}
	} else {
		next.CreatedAt = orderCreatedAt(data["created_at"], eventAt)
	}

	// Newness is decided when an order first becomes an extended order for a
	// customer: on creation, when a v1 order is upgraded by a v2 update, or if an
	// update reassigns the order to another customer.
	if next.Extended && next.CustomerID != "" &&
		(!exists || !prior.Extended || prior.CustomerID != next.CustomerID) {
		known, err := customerKnown(ctx, tx, tenantID, next.CustomerID)
		if err != nil {
			return err
		}
		next.IsNewCustomer = !known
	} else if !next.Extended || next.CustomerID == "" {
		next.IsNewCustomer = false
	}

	var old *Order
	if exists {
		old = prior
	}
	if err := apply(ctx, tx, tenantID, next.CreatedAt, old, &next); err != nil {
		return err
	}
	return saveState(ctx, tx, tenantID, &next)
}

// customerKnown reports whether the customer already has any order for the
// tenant. customer_aggregates gets a row for every extended order the customer
// places (cancelled ones included), so a primary-key lookup answers it.
func customerKnown(ctx context.Context, tx *sql.Tx, tenantID, customerID string) (bool, error) {
	var known bool
	err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM customer_aggregates WHERE tenant_id = $1 AND customer_id = $2)`,
		tenantID, customerID).Scan(&known)
	return known, err
}

// ─── order_state ─────────────────────────────────────────────────────────────

func loadState(ctx context.Context, tx *sql.Tx, tenantID, orderID string) (*Order, bool, error) {
	o := &Order{OrderID: orderID}
	// ROUND(x*100)::bigint turns NUMERIC into exact integer cents.
	err := tx.QueryRowContext(ctx, `
		SELECT ROUND(amount*100)::bigint, currency, status, product_id, product_name, category, occurred_at,
		       has_extended, is_new_customer,
		       customer_id, customer_tier, payment_method, payment_status, discount_code,
		       ROUND(discount_amount*100)::bigint, ROUND(tax_amount*100)::bigint, ROUND(shipping_cost*100)::bigint,
		       quantity, sku, fulfillment_status, return_reason, country, state_province, city, channel, referrer_source
		FROM order_state
		WHERE tenant_id = $1 AND order_id = $2
		FOR UPDATE`, tenantID, orderID).Scan(
		&o.AmountC, &o.Currency, &o.Status, &o.ProductID, &o.ProductName, &o.Category, &o.CreatedAt,
		&o.Extended, &o.IsNewCustomer,
		&o.CustomerID, &o.CustomerTier, &o.PaymentMethod, &o.PaymentStatus, &o.DiscountCode,
		&o.DiscountC, &o.TaxC, &o.ShippingC,
		&o.Quantity, &o.SKU, &o.FulfillmentStatus, &o.ReturnReason, &o.Country, &o.StateProvince, &o.City,
		&o.Channel, &o.ReferrerSource)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("load order state: %w", err)
	}
	o.CreatedAt = o.CreatedAt.UTC()
	return o, true, nil
}

// saveState upserts order_state. occurred_at is deliberately not updated on
// conflict: it records when the order was created.
func saveState(ctx context.Context, tx *sql.Tx, tenantID string, o *Order) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO order_state (
			order_id, tenant_id, amount, currency, status, product_id, product_name, category, occurred_at, updated_at,
			has_extended, is_new_customer,
			customer_id, customer_tier, payment_method, payment_status, discount_code,
			discount_amount, tax_amount, shipping_cost, quantity, sku, fulfillment_status, return_reason,
			country, state_province, city, channel, referrer_source)
		VALUES ($1,$2,$3::bigint/100.0,$4,$5,$6,$7,$8,$9,NOW(),
			$10,$11,
			$12,$13,$14,$15,$16,
			$17::bigint/100.0,$18::bigint/100.0,$19::bigint/100.0,$20,$21,$22,$23,
			$24,$25,$26,$27,$28)
		ON CONFLICT (tenant_id, order_id) DO UPDATE SET
			amount = EXCLUDED.amount, currency = EXCLUDED.currency, status = EXCLUDED.status,
			product_id = EXCLUDED.product_id, product_name = EXCLUDED.product_name, category = EXCLUDED.category,
			updated_at = NOW(),
			has_extended = EXCLUDED.has_extended, is_new_customer = EXCLUDED.is_new_customer,
			customer_id = EXCLUDED.customer_id, customer_tier = EXCLUDED.customer_tier,
			payment_method = EXCLUDED.payment_method, payment_status = EXCLUDED.payment_status,
			discount_code = EXCLUDED.discount_code, discount_amount = EXCLUDED.discount_amount,
			tax_amount = EXCLUDED.tax_amount, shipping_cost = EXCLUDED.shipping_cost,
			quantity = EXCLUDED.quantity, sku = EXCLUDED.sku,
			fulfillment_status = EXCLUDED.fulfillment_status, return_reason = EXCLUDED.return_reason,
			country = EXCLUDED.country, state_province = EXCLUDED.state_province, city = EXCLUDED.city,
			channel = EXCLUDED.channel, referrer_source = EXCLUDED.referrer_source`,
		o.OrderID, tenantID, o.AmountC, o.Currency, o.Status, o.ProductID, o.ProductName, o.Category, o.CreatedAt,
		o.Extended, o.IsNewCustomer,
		o.CustomerID, o.CustomerTier, o.PaymentMethod, o.PaymentStatus, o.DiscountCode,
		o.DiscountC, o.TaxC, o.ShippingC, o.Quantity, o.SKU, o.FulfillmentStatus, o.ReturnReason,
		o.Country, o.StateProvince, o.City, o.Channel, o.ReferrerSource)
	if err != nil {
		return fmt.Errorf("upsert order state: %w", err)
	}
	return nil
}

// ─── Accumulating contributions ──────────────────────────────────────────────

// row is one pending change to one aggregate row: a key and a vector of deltas.
type row struct {
	key   []interface{}
	v     []int64
	attrs []interface{} // descriptive columns taken from the new side
	// touch rows apply even when every delta is zero (customer first/last order).
	touch bool
	first time.Time
	last  time.Time
	tier  string
}

// acc sums contributions per aggregate row, so an update that leaves a row
// unchanged cancels out and issues no SQL.
type acc struct {
	rows  map[string]*row
	order []string
	width int
}

func newAcc(width int) *acc { return &acc{rows: map[string]*row{}, width: width} }

func (a *acc) add(sign int64, key []interface{}, v []int64) *row {
	var sb strings.Builder
	for _, k := range key {
		fmt.Fprintf(&sb, "%v\x1f", k)
	}
	id := sb.String()
	r, ok := a.rows[id]
	if !ok {
		r = &row{key: key, v: make([]int64, a.width)}
		a.rows[id] = r
		a.order = append(a.order, id)
	}
	for i := range v {
		r.v[i] += sign * v[i]
	}
	return r
}

func (r *row) zero() bool {
	for _, x := range r.v {
		if x != 0 {
			return false
		}
	}
	return true
}

// each calls fn for every row that actually changes, in insertion order.
func (a *acc) each(fn func(*row) error) error {
	for _, id := range a.order {
		r := a.rows[id]
		if r.zero() && !r.touch {
			continue
		}
		if err := fn(r); err != nil {
			return err
		}
	}
	return nil
}

func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func day(t time.Time) string { return t.UTC().Format("2006-01-02") }

// apply books contribution(next) - contribution(prev) into every aggregate table.
// prev is nil when the order is new.
func apply(ctx context.Context, tx *sql.Tx, tenantID string, eventAt time.Time, prev, next *Order) error {
	if err := applySummary(ctx, tx, tenantID, eventAt, prev, next); err != nil {
		return err
	}
	if err := applyProduct(ctx, tx, tenantID, prev, next); err != nil {
		return err
	}
	if err := applyCustomer(ctx, tx, tenantID, prev, next); err != nil {
		return err
	}
	return applyCohortTables(ctx, tx, tenantID, prev, next)
}

// ─── Flow tables: order_summary_hourly / order_summary_daily ─────────────────
//
// Changes adjust the original order's creation bucket, including cancellations.
// This keeps trend totals consistent with the date-scoped cohort tables.

const summaryWidth = 10

// summaryVec is the order's contribution. Money is in cents.
// Columns: total, active, cancelled, revenue, returned, discount, tax, shipping, quantity, new_customer_orders.
func summaryVec(o *Order) []int64 {
	live := !o.cancelled()
	v := make([]int64, summaryWidth)
	v[0] = 1
	v[1] = b2i(live)
	v[2] = b2i(!live)
	if live {
		v[3] = o.AmountC
	}
	if o.Extended && live {
		v[4] = b2i(o.returned())
		v[5], v[6], v[7] = o.DiscountC, o.TaxC, o.ShippingC
		v[8] = o.Quantity
		v[9] = b2i(o.IsNewCustomer && o.CustomerID != "")
	}
	return v
}

func applySummary(ctx context.Context, tx *sql.Tx, tenantID string, eventAt time.Time, prev, next *Order) error {
	d := summaryVec(next)
	if prev != nil {
		p := summaryVec(prev)
		for i := range d {
			d[i] -= p[i]
		}
	}
	zero := true
	for _, x := range d {
		if x != 0 {
			zero = false
		}
	}
	if zero {
		return nil
	}
	for _, t := range []struct {
		table  string
		bucket interface{}
		cast   string
	}{
		{"order_summary_hourly", eventAt.Truncate(time.Hour), ""},
		{"order_summary_daily", day(eventAt), "::date"},
	} {
		q := fmt.Sprintf(`
			INSERT INTO %[1]s (tenant_id, bucket, total_orders, active_orders, cancelled_orders, total_revenue, currency,
				returned_orders, total_discount, total_tax, total_shipping, total_quantity, new_customer_orders)
			VALUES ($1, $2%[2]s, $3, $4, $5, $6::bigint/100.0, $7, $8, $9::bigint/100.0, $10::bigint/100.0, $11::bigint/100.0, $12, $13)
			ON CONFLICT (tenant_id, bucket) DO UPDATE SET
				total_orders = %[1]s.total_orders + EXCLUDED.total_orders,
				active_orders = %[1]s.active_orders + EXCLUDED.active_orders,
				cancelled_orders = %[1]s.cancelled_orders + EXCLUDED.cancelled_orders,
				total_revenue = %[1]s.total_revenue + EXCLUDED.total_revenue,
				returned_orders = %[1]s.returned_orders + EXCLUDED.returned_orders,
				total_discount = %[1]s.total_discount + EXCLUDED.total_discount,
				total_tax = %[1]s.total_tax + EXCLUDED.total_tax,
				total_shipping = %[1]s.total_shipping + EXCLUDED.total_shipping,
				total_quantity = %[1]s.total_quantity + EXCLUDED.total_quantity,
				new_customer_orders = %[1]s.new_customer_orders + EXCLUDED.new_customer_orders`,
			t.table, t.cast)
		if _, err := tx.ExecContext(ctx, q, tenantID, t.bucket, d[0], d[1], d[2], d[3], next.Currency,
			d[4], d[5], d[6], d[7], d[8], d[9]); err != nil {
			return fmt.Errorf("upsert %s: %w", t.table, err)
		}
	}
	return nil
}

// ─── product_aggregates ──────────────────────────────────────────────────────

const productWidth = 5

// Columns: order_count, revenue, quantity_sold, return_count, discount.
func productVec(o *Order) []int64 {
	v := make([]int64, productWidth)
	if o.cancelled() {
		return v
	}
	v[0], v[1] = 1, o.AmountC
	if o.Extended {
		v[2], v[3], v[4] = o.Quantity, b2i(o.returned()), o.DiscountC
	}
	return v
}

func applyProduct(ctx context.Context, tx *sql.Tx, tenantID string, prev, next *Order) error {
	a := newAcc(productWidth)
	if next.ProductID != "" {
		r := a.add(1, []interface{}{next.ProductID}, productVec(next))
		r.attrs = []interface{}{next.ProductName, next.Category}
		// Always write the product row, even with zero deltas, so a rename or
		// category change reaches product_aggregates.
		r.touch = true
	}
	if prev != nil && prev.ProductID != "" {
		r := a.add(-1, []interface{}{prev.ProductID}, productVec(prev))
		if r.attrs == nil {
			r.attrs = []interface{}{prev.ProductName, prev.Category}
		}
	}
	return a.each(func(r *row) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO product_aggregates (tenant_id, product_id, product_name, category,
				order_count, total_revenue, quantity_sold, return_count, total_discount)
			VALUES ($1, $2, $3, $4, $5, $6::bigint/100.0, $7, $8, $9::bigint/100.0)
			ON CONFLICT (tenant_id, product_id) DO UPDATE SET
				order_count = product_aggregates.order_count + EXCLUDED.order_count,
				total_revenue = product_aggregates.total_revenue + EXCLUDED.total_revenue,
				quantity_sold = product_aggregates.quantity_sold + EXCLUDED.quantity_sold,
				return_count = product_aggregates.return_count + EXCLUDED.return_count,
				total_discount = product_aggregates.total_discount + EXCLUDED.total_discount,
				product_name = EXCLUDED.product_name, category = EXCLUDED.category`,
			tenantID, r.key[0], r.attrs[0], r.attrs[1], r.v[0], r.v[1], r.v[2], r.v[3], r.v[4])
		if err != nil {
			return fmt.Errorf("upsert product_aggregates: %w", err)
		}
		return nil
	})
}

// ─── customer_aggregates ─────────────────────────────────────────────────────

const customerWidth = 4

// Columns: order_count, spend, discount, quantity. Only extended orders with a
// customer id take part; cancelled orders add nothing to the sums.
func customerVec(o *Order) []int64 {
	v := make([]int64, customerWidth)
	if o.cancelled() {
		return v
	}
	v[0], v[1], v[2], v[3] = 1, o.AmountC, o.DiscountC, o.Quantity
	return v
}

func hasCustomer(o *Order) bool { return o != nil && o.Extended && o.CustomerID != "" }

func applyCustomer(ctx context.Context, tx *sql.Tx, tenantID string, prev, next *Order) error {
	a := newAcc(customerWidth)
	if hasCustomer(next) {
		r := a.add(1, []interface{}{next.CustomerID}, customerVec(next))
		// first/last order and tier come from the new side only: they are
		// monotone and cannot be un-done by a later change.
		r.touch, r.first, r.last, r.tier = true, next.CreatedAt, next.CreatedAt, next.CustomerTier
	}
	if hasCustomer(prev) {
		a.add(-1, []interface{}{prev.CustomerID}, customerVec(prev))
	}
	return a.each(func(r *row) error {
		var first, last interface{}
		tier := "regular"
		if r.touch {
			first, last, tier = r.first, r.last, r.tier
		}
		// A removal-only row (the order moved to another customer) never inserts:
		// the customer necessarily already exists.
		_, err := tx.ExecContext(ctx, `
			INSERT INTO customer_aggregates (tenant_id, customer_id, customer_tier, order_count, total_spend,
				total_discount, total_quantity, first_order_at, last_order_at)
			VALUES ($1, $2, $3, $4, $5::bigint/100.0, $6::bigint/100.0, $7, $8, $9)
			ON CONFLICT (tenant_id, customer_id) DO UPDATE SET
				order_count = customer_aggregates.order_count + EXCLUDED.order_count,
				total_spend = customer_aggregates.total_spend + EXCLUDED.total_spend,
				total_discount = customer_aggregates.total_discount + EXCLUDED.total_discount,
				total_quantity = customer_aggregates.total_quantity + EXCLUDED.total_quantity,
				first_order_at = LEAST(customer_aggregates.first_order_at, EXCLUDED.first_order_at),
				last_order_at = GREATEST(customer_aggregates.last_order_at, EXCLUDED.last_order_at),
				customer_tier = CASE
					WHEN EXCLUDED.last_order_at IS NOT NULL
					 AND (customer_aggregates.last_order_at IS NULL OR EXCLUDED.last_order_at >= customer_aggregates.last_order_at)
					THEN EXCLUDED.customer_tier ELSE customer_aggregates.customer_tier END`,
			tenantID, r.key[0], tier, r.v[0], r.v[1], r.v[2], r.v[3], first, last)
		if err != nil {
			return fmt.Errorf("upsert customer_aggregates: %w", err)
		}
		return nil
	})
}

// ─── Cohort tables ───────────────────────────────────────────────────────────
//
// geo, channel, payment, return, fulfillment, cohort_daily and discount are all
// keyed by the day the order was CREATED. A row therefore always equals a
// GROUP BY over order_state for orders created that day, which is also how the
// verification checks them.

func applyCohortTables(ctx context.Context, tx *sql.Tx, tenantID string, prev, next *Order) error {
	type spec struct {
		table string
		width int
		build func(o *Order, add func(key []interface{}, v []int64))
		sql   string
	}
	specs := []spec{
		{"geo_aggregates", 3, geoContrib, `
			INSERT INTO geo_aggregates (tenant_id, bucket, country, state_province, city, order_count, total_revenue, total_quantity)
			VALUES ($1, $2::date, $3, $4, $5, $6, $7::bigint/100.0, $8)
			ON CONFLICT (tenant_id, bucket, country, state_province, city) DO UPDATE SET
				order_count = geo_aggregates.order_count + EXCLUDED.order_count,
				total_revenue = geo_aggregates.total_revenue + EXCLUDED.total_revenue,
				total_quantity = geo_aggregates.total_quantity + EXCLUDED.total_quantity`},
		{"channel_aggregates", 3, channelContrib, `
			INSERT INTO channel_aggregates (tenant_id, bucket, channel, referrer_source, order_count, total_revenue, new_customers)
			VALUES ($1, $2::date, $3, $4, $5, $6::bigint/100.0, $7)
			ON CONFLICT (tenant_id, bucket, channel, referrer_source) DO UPDATE SET
				order_count = channel_aggregates.order_count + EXCLUDED.order_count,
				total_revenue = channel_aggregates.total_revenue + EXCLUDED.total_revenue,
				new_customers = channel_aggregates.new_customers + EXCLUDED.new_customers`},
		{"payment_aggregates", 2, paymentContrib, `
			INSERT INTO payment_aggregates (tenant_id, bucket, payment_method, payment_status, order_count, total_amount)
			VALUES ($1, $2::date, $3, $4, $5, $6::bigint/100.0)
			ON CONFLICT (tenant_id, bucket, payment_method, payment_status) DO UPDATE SET
				order_count = payment_aggregates.order_count + EXCLUDED.order_count,
				total_amount = payment_aggregates.total_amount + EXCLUDED.total_amount`},
		{"return_aggregates", 2, returnContrib, `
			INSERT INTO return_aggregates (tenant_id, bucket, return_reason, return_count, refund_amount)
			VALUES ($1, $2::date, $3, $4, $5::bigint/100.0)
			ON CONFLICT (tenant_id, bucket, return_reason) DO UPDATE SET
				return_count = return_aggregates.return_count + EXCLUDED.return_count,
				refund_amount = return_aggregates.refund_amount + EXCLUDED.refund_amount`},
		{"fulfillment_aggregates", 1, fulfillmentContrib, `
			INSERT INTO fulfillment_aggregates (tenant_id, bucket, fulfillment_status, order_count)
			VALUES ($1, $2::date, $3, $4)
			ON CONFLICT (tenant_id, bucket, fulfillment_status) DO UPDATE SET
				order_count = fulfillment_aggregates.order_count + EXCLUDED.order_count`},
		{"cohort_daily", 2, cohortContrib, `
			INSERT INTO cohort_daily (tenant_id, bucket, customer_type, order_count, total_revenue)
			VALUES ($1, $2::date, $3, $4, $5::bigint/100.0)
			ON CONFLICT (tenant_id, bucket, customer_type) DO UPDATE SET
				order_count = cohort_daily.order_count + EXCLUDED.order_count,
				total_revenue = cohort_daily.total_revenue + EXCLUDED.total_revenue`},
		{"discount_aggregates", 2, discountContrib, `
			INSERT INTO discount_aggregates (tenant_id, bucket, discount_code, uses, total_discount)
			VALUES ($1, $2::date, $3, $4, $5::bigint/100.0)
			ON CONFLICT (tenant_id, bucket, discount_code) DO UPDATE SET
				uses = discount_aggregates.uses + EXCLUDED.uses,
				total_discount = discount_aggregates.total_discount + EXCLUDED.total_discount`},
	}

	for _, s := range specs {
		a := newAcc(s.width)
		if next != nil && next.Extended {
			s.build(next, func(key []interface{}, v []int64) { a.add(1, key, v) })
		}
		if prev != nil && prev.Extended {
			s.build(prev, func(key []interface{}, v []int64) { a.add(-1, key, v) })
		}
		err := a.each(func(r *row) error {
			args := make([]interface{}, 0, len(r.key)+len(r.v)+1)
			args = append(args, tenantID)
			args = append(args, r.key...)
			for _, x := range r.v {
				args = append(args, x)
			}
			_, err := tx.ExecContext(ctx, s.sql, args...)
			return err
		})
		if err != nil {
			return fmt.Errorf("upsert %s: %w", s.table, err)
		}
	}
	return nil
}

// Each contributor emits (key without tenant_id, deltas). Keys start with the
// creation-day bucket. Cancelled orders are excluded from everything except the
// payment and fulfilment tables: a failed payment or a cancellation is exactly
// what those two measure.

func geoContrib(o *Order, add func([]interface{}, []int64)) {
	if o.cancelled() {
		return
	}
	add([]interface{}{day(o.CreatedAt), o.Country, o.StateProvince, o.City}, []int64{1, o.AmountC, o.Quantity})
}

func channelContrib(o *Order, add func([]interface{}, []int64)) {
	if o.cancelled() {
		return
	}
	add([]interface{}{day(o.CreatedAt), o.Channel, o.ReferrerSource},
		[]int64{1, o.AmountC, b2i(o.IsNewCustomer && o.CustomerID != "")})
}

func paymentContrib(o *Order, add func([]interface{}, []int64)) {
	add([]interface{}{day(o.CreatedAt), o.PaymentMethod, o.PaymentStatus}, []int64{1, o.AmountC})
}

// returnContrib: the refund is what the customer actually paid for the goods,
// the order amount less any discount. Tax and shipping are not included.
func returnContrib(o *Order, add func([]interface{}, []int64)) {
	if o.cancelled() || !o.returned() {
		return
	}
	add([]interface{}{day(o.CreatedAt), o.ReturnReason}, []int64{1, o.AmountC - o.DiscountC})
}

func fulfillmentContrib(o *Order, add func([]interface{}, []int64)) {
	add([]interface{}{day(o.CreatedAt), o.FulfillmentStatus}, []int64{1})
}

func cohortContrib(o *Order, add func([]interface{}, []int64)) {
	if o.cancelled() || o.CustomerID == "" {
		return
	}
	kind := customerReturning
	if o.IsNewCustomer {
		kind = customerNew
	}
	add([]interface{}{day(o.CreatedAt), kind}, []int64{1, o.AmountC})
}

func discountContrib(o *Order, add func([]interface{}, []int64)) {
	if o.cancelled() || o.DiscountCode == "" {
		return
	}
	add([]interface{}{day(o.CreatedAt), o.DiscountCode}, []int64{1, o.DiscountC})
}
