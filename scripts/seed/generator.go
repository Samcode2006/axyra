package main

// Row generation for the bulk seeder.
//
// Each order is built so its fields agree with each other (docs/schema-expansion-plan.md
// section 12): the discount exists only when a code was used, tax is 18% of the
// discounted amount, shipping is free for COD and wallet orders, the fulfilment
// stage follows the status, and a return reason exists only on returned orders.
// All money is computed in integer cents and written as exact NUMERIC.

import (
	"fmt"
	"math/big"
	"math/rand"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// weighted is a discrete distribution over string values.
type weighted struct {
	values []string
	cum    []float64
}

func newWeighted(values []string, weights []float64) weighted {
	if len(values) != len(weights) {
		panic("weighted: values and weights differ in length")
	}
	total := 0.0
	for _, w := range weights {
		total += w
	}
	cum := make([]float64, len(weights))
	run := 0.0
	for i, w := range weights {
		run += w / total
		cum[i] = run
	}
	cum[len(cum)-1] = 1 // guard against float rounding
	return weighted{values: values, cum: cum}
}

func (w weighted) pick(r *rand.Rand) string { return w.at(r.Float64()) }

// at maps u in [0,1) to a value. Used directly with a hash to make a
// customer's attributes stable across their orders.
func (w weighted) at(u float64) string {
	for i, c := range w.cum {
		if u < c {
			return w.values[i]
		}
	}
	return w.values[len(w.values)-1]
}

var (
	tierDist     = newWeighted([]string{"free", "silver", "gold", "platinum"}, []float64{40, 30, 20, 10})
	methodDist   = newWeighted([]string{"upi", "card", "wallet", "cod", "bnpl", "netbanking"}, []float64{35, 30, 15, 12, 5, 3})
	payStatDist  = newWeighted([]string{"paid", "pending", "failed", "refunded", "chargeback"}, []float64{88, 5, 4, 2, 1})
	quantityDist = newWeighted([]string{"1", "2", "3", "4"}, []float64{70, 18, 8, 4})
	channelDist  = newWeighted([]string{"web", "mobile_app", "pos", "marketplace", "api"}, []float64{45, 35, 10, 8, 2})
	referrerDist = newWeighted([]string{"direct", "google", "instagram", "email", "affiliate", "whatsapp"}, []float64{30, 25, 18, 12, 10, 5})
	countryDist  = newWeighted([]string{"IN", "US", "SG", "AE", "GB", "AU"}, []float64{70, 12, 5, 5, 4, 4})
	reasonDist   = newWeighted([]string{"defective", "wrong_item", "changed_mind", "damaged", "other"}, []float64{35, 25, 20, 15, 5})
)

// discountCodes is the promo pool; a quarter of orders use one.
var discountCodes = []string{
	"SALE20", "FLAT100", "WELCOME50", "FESTIVE15", "NEWUSER10",
	"FREESHIP", "LOYALTY5", "WEEKEND25", "APP10", "BIGSAVE30",
}

const discountUseRate = 0.25

// places lists realistic states and cities per country.
var places = map[string]map[string][]string{
	"IN": {
		"Maharashtra": {"Mumbai", "Pune", "Nagpur"},
		"Karnataka":   {"Bengaluru", "Mysuru"},
		"Delhi":       {"New Delhi"},
		"Tamil Nadu":  {"Chennai", "Coimbatore"},
		"Telangana":   {"Hyderabad"},
		"Gujarat":     {"Ahmedabad", "Surat"},
	},
	"US": {
		"California": {"San Francisco", "Los Angeles"},
		"Texas":      {"Austin", "Dallas"},
		"New York":   {"New York"},
	},
	"SG": {"Singapore": {"Singapore"}},
	"AE": {"Dubai": {"Dubai"}, "Abu Dhabi": {"Abu Dhabi"}},
	"GB": {"England": {"London", "Manchester"}},
	"AU": {"New South Wales": {"Sydney"}, "Victoria": {"Melbourne"}},
}

// Stable iteration order for the maps above: Go randomizes map order, which
// would break reproducibility with a fixed -seed.
var placeStates = map[string][]string{}

func init() {
	for country, states := range places {
		names := make([]string, 0, len(states))
		for s := range states {
			names = append(names, s)
		}
		sortStrings(names)
		placeStates[country] = names
	}
}

func sortStrings(a []string) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

var (
	colours = []string{"BLK", "WHT", "BLU", "RED", "GRY"}
	sizes   = []string{"S", "M", "L", "XL"}
)

// ─── Customers ───────────────────────────────────────────────────────────────

const customerPool = 10_000

// customerRepeatShare is the share of orders placed by repeat buyers, who are
// drawn from the pool with a Zipf distribution (a few customers order a lot).
// All other orders come from a uniform draw over the same pool.
const customerRepeatShare = 0.30

// mix is a fixed integer hash (splitmix64). It is used only to derive a
// customer's tier from their number, never for their identity.
func mix(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

// customerID is unique by construction: it is the customer's number, padded.
// (Hashing the number into a short id would risk two customers colliding.)
func customerID(n int) string { return fmt.Sprintf("cust_%06d", n) }

// customerTier is a property of the customer, not of the order, so it stays the
// same across their orders.
func customerTier(n int) string {
	return tierDist.at(float64(mix(uint64(n)^0xA5A5)&0xffffff) / float64(1<<24))
}

// ─── Generator ───────────────────────────────────────────────────────────────

// generator produces order rows for one worker. It is not safe for concurrent use.
type generator struct {
	rng  *rand.Rand
	zipf *rand.Zipf
	cfg  config
}

func newGenerator(seed int64, cfg config) *generator {
	rng := rand.New(rand.NewSource(seed))
	return &generator{
		rng:  rng,
		zipf: rand.NewZipf(rng, 1.2, 1, customerPool-1),
		cfg:  cfg,
	}
}

func numeric(cents int64) pgtype.Numeric {
	return pgtype.Numeric{Int: big.NewInt(cents), Exp: -2, Valid: true}
}

// orderColumns is the COPY column list. Its order must match row().
var orderColumns = []string{
	"id", "store_id", "amount", "currency", "status", "product_id", "product_name", "category",
	"customer_id", "customer_email", "customer_tier",
	"payment_method", "payment_status", "discount_code", "discount_amount", "tax_amount", "shipping_cost",
	"quantity", "sku", "fulfillment_status", "return_reason",
	"country", "state_province", "city", "channel", "referrer_source",
	"created_at", "updated_at",
}

// fulfillmentFor derives the fulfilment stage from an order status.
func fulfillmentFor(status string) string {
	switch status {
	case "confirmed":
		return "processing"
	case "shipped", "delivered", "cancelled", "returned":
		return status
	default:
		return "pending"
	}
}

// status picks an order status. cancelRate is checked first so the configured
// cancellation share holds regardless of the other weights. The remaining
// weights are renormalized over the non-cancelled statuses; without that the
// leftover probability would all fall through to the last status ("returned").
func (g *generator) status() string {
	if g.rng.Float64() < g.cfg.cancelRate {
		return "cancelled"
	}
	var total float64
	for i, w := range statusWeights {
		if statuses[i] != "cancelled" {
			total += w
		}
	}
	r := g.rng.Float64() * total
	cum := 0.0
	picked := statuses[len(statuses)-1]
	for i, w := range statusWeights {
		if statuses[i] == "cancelled" {
			continue
		}
		cum += w
		if r < cum {
			picked = statuses[i]
			break
		}
	}
	return picked
}

// row builds one order. storeID has already been chosen by the caller.
func (g *generator) row(storeID string, createdAt time.Time) []any {
	r := g.rng
	p := &products[r.Intn(len(products))]
	status := g.status()

	// Money, in cents. amount is the line total: unit price x quantity.
	qty, _ := atoiSmall(quantityDist.pick(r))
	minC, maxC := int64(p.minPrice*100), int64(p.maxPrice*100)
	unitC := minC + r.Int63n(maxC-minC+1)
	amountC := unitC * int64(qty)

	method := methodDist.pick(r)
	var code string
	var discountC int64
	if r.Float64() < discountUseRate {
		code = discountCodes[r.Intn(len(discountCodes))]
		pctBP := int64(500 + r.Intn(2501)) // 5.00% to 30.00%, in basis points
		discountC = amountC * pctBP / 10000
	}
	taxC := (amountC - discountC) * 18 / 100 // 18% GST on the discounted amount
	var shippingC int64
	if method != "cod" && method != "wallet" {
		shippingC = int64(r.Intn(200)) * 100 // 0 to 199 rupees
	}

	// Customer: 30% of orders come from Zipf-distributed repeat buyers.
	var cn int
	if r.Float64() < customerRepeatShare {
		cn = int(g.zipf.Uint64())
	} else {
		cn = r.Intn(customerPool)
	}

	// Place: country first, then a state of that country, then a city of that state.
	country := countryDist.pick(r)
	states := placeStates[country]
	state := states[r.Intn(len(states))]
	cities := places[country][state]
	city := cities[r.Intn(len(cities))]

	var reason string
	if status == "returned" {
		reason = reasonDist.pick(r)
	}

	colour := colours[(productIndex(p.id)+r.Intn(3))%len(colours)]
	size := sizes[(productIndex(p.id)+r.Intn(3))%len(sizes)]

	return []any{
		fmt.Sprintf("order_%x%x", r.Int63(), r.Int31()),
		storeID,
		numeric(amountC),
		"INR",
		status,
		p.id,
		p.name,
		p.category,
		customerID(cn),
		fmt.Sprintf("user%d@example.com", cn), // synthetic; never a real address
		customerTier(cn),
		method,
		payStatDist.pick(r),
		code,
		numeric(discountC),
		numeric(taxC),
		numeric(shippingC),
		int32(qty),
		fmt.Sprintf("%s-%s-%s", p.id, colour, size),
		fulfillmentFor(status),
		reason,
		country,
		state,
		city,
		channelDist.pick(r),
		referrerDist.pick(r),
		createdAt,
		createdAt,
	}
}

func atoiSmall(s string) (int, bool) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

// productIndex turns "prod_007" into 7 (0 when the id has no number).
func productIndex(id string) int {
	n := 0
	for _, c := range id {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}
