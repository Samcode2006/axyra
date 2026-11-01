package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Only configured source databases are reachable; clients cannot supply SQL or DSNs.
func (h *Handler) comparisonDSNs() (map[string]string, error) {
	sources := map[string]string{}
	if h.cfg.ComparisonSources == "" {
		if h.cfg.ComparisonReferenceDSN != "" {
			for i := 1; i <= h.cfg.TenantCount; i++ {
				sources[fmt.Sprintf("tenant_%03d", i)] = strings.ReplaceAll(h.cfg.ComparisonReferenceDSN, "{index}", fmt.Sprint(i))
			}
		}
		return sources, nil
	}
	err := json.Unmarshal([]byte(h.cfg.ComparisonSources), &sources)
	return sources, err
}

func (h *Handler) ComparisonSources(w http.ResponseWriter, r *http.Request) {
	p := principal(w, r)
	if p == nil {
		return
	}
	sources, err := h.comparisonDSNs()
	if err != nil {
		respondError(w, 503, "Comparison source configuration is invalid")
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT id,name,external_tenant_id FROM tenants WHERE ($1 OR organization_id=$2) ORDER BY name,id`, p.IsOperator(), p.OrgID)
	if err != nil {
		serverError(w, r, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, name, store string
		if err = rows.Scan(&id, &name, &store); err != nil {
			serverError(w, r, err)
			return
		}
		items = append(items, map[string]any{"id": id, "name": name, "store": store, "configured": sources[id] != ""})
	}
	if err = rows.Err(); err != nil {
		serverError(w, r, err)
		return
	}
	respondJSON(w, 200, items)
}

func (h *Handler) comparisonSource(w http.ResponseWriter, r *http.Request, id string) (*sql.DB, string) {
	p := principal(w, r)
	if p == nil {
		return nil, ""
	}
	if id == "" {
		respondError(w, 400, "Select a tenant")
		return nil, ""
	}
	var store string
	err := h.db.QueryRowContext(r.Context(), `SELECT external_tenant_id FROM tenants WHERE id=$1 AND ($2 OR organization_id=$3)`, id, p.IsOperator(), p.OrgID).Scan(&store)
	if err == sql.ErrNoRows {
		notFound(w, "Tenant")
		return nil, ""
	}
	if err != nil {
		serverError(w, r, err)
		return nil, ""
	}
	sources, err := h.comparisonDSNs()
	if err != nil {
		respondError(w, 503, "Comparison source configuration is invalid")
		return nil, ""
	}
	if sources[id] == "" {
		respondError(w, 409, "This tenant has no comparison source. Configure AXYRA_COMPARISON_SOURCES on the control plane.")
		return nil, ""
	}
	db, err := sql.Open("postgres", sources[id])
	if err != nil {
		respondError(w, 503, "Cannot open the configured source database")
		return nil, ""
	}
	db.SetMaxOpenConns(1)
	return db, store
}

type comparisonProduct struct {
	ID      string      `json:"product_id"`
	Count   int64       `json:"order_count"`
	Revenue json.Number `json:"revenue"`
}
type comparisonSample struct {
	Source      []comparisonProduct `json:"source"`
	Analytics   []comparisonProduct `json:"analytics"`
	Match       bool                `json:"match"`
	Stable      bool                `json:"source_stable"`
	Differences []string            `json:"differences"`
	SourceMS    float64             `json:"source_ms"`
	AnalyticsMS float64             `json:"analytics_ms"`
	CheckedAt   time.Time           `json:"checked_at"`
}

type comparisonQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func comparisonRows(ctx context.Context, db comparisonQuerier, store string, from, to time.Time) ([]comparisonProduct, float64, error) {
	query := `SELECT product_id,COUNT(*) FILTER(WHERE status<>'cancelled'),COALESCE(SUM(amount) FILTER(WHERE status<>'cancelled'),0)::text FROM public.orders WHERE store_id=$1 AND product_id<>''`
	args := []any{store}
	if !from.IsZero() {
		query += ` AND created_at >= $2 AND created_at <= $3`
		args = append(args, from, to)
	}
	query += ` GROUP BY product_id ORDER BY COALESCE(SUM(amount) FILTER(WHERE status<>'cancelled'),0) DESC, product_id LIMIT 100`
	started := time.Now()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := []comparisonProduct{}
	for rows.Next() {
		var p comparisonProduct
		var amount string
		if err = rows.Scan(&p.ID, &p.Count, &amount); err != nil {
			return nil, 0, err
		}
		p.Revenue = json.Number(amount)
		result = append(result, p)
	}
	err = rows.Err()
	if closeErr := rows.Close(); err == nil {
		err = closeErr
	}
	return result, float64(time.Since(started)) / float64(time.Millisecond), err
}

func comparisonDiff(a, b []comparisonProduct) []string {
	diffs := []string{}
	actual := map[string]comparisonProduct{}
	for _, p := range b {
		actual[p.ID] = p
	}
	for i, p := range a {
		q, ok := actual[p.ID]
		if !ok {
			diffs = append(diffs, p.ID+": missing from Axyra")
			continue
		}
		delete(actual, p.ID)
		x, xok := new(big.Rat).SetString(p.Revenue.String())
		y, yok := new(big.Rat).SetString(q.Revenue.String())
		if p.Count != q.Count || !xok || !yok || x.Cmp(y) != 0 {
			diffs = append(diffs, fmt.Sprintf("%s: SQL %d orders / %s; Axyra %d orders / %s", p.ID, p.Count, p.Revenue, q.Count, q.Revenue))
		}
		if i >= len(b) || b[i].ID != p.ID {
			diffs = append(diffs, p.ID+": ranking differs")
		}
	}
	for _, p := range b {
		if _, ok := actual[p.ID]; ok {
			diffs = append(diffs, p.ID+": present only in Axyra")
		}
	}
	return diffs
}

func comparisonRange(r *http.Request) (time.Time, time.Time, error) {
	a, b := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	if a == "" && b == "" {
		return time.Time{}, time.Time{}, nil
	}
	from, e1 := time.Parse(time.RFC3339Nano, a)
	to, e2 := time.Parse(time.RFC3339Nano, b)
	if e1 != nil || e2 != nil || from.After(to) {
		return time.Time{}, time.Time{}, fmt.Errorf("Supply valid from/to timestamps in order")
	}
	return from, to, nil
}

// Never fall back to HTTP duration when the backend did not supply DB timing.
func comparisonDBDuration(header http.Header) (float64, error) {
	ms, err := strconv.ParseFloat(header.Get("X-DB-Query-Ms"), 64)
	if err != nil || math.IsNaN(ms) || math.IsInf(ms, 0) || ms < 0 {
		return 0, fmt.Errorf("missing or invalid DB query duration")
	}
	return ms, nil
}

func (h *Handler) ComparisonSnapshot(w http.ResponseWriter, r *http.Request) {
	from, to, err := comparisonRange(r)
	if err != nil {
		respondError(w, 400, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	id := r.URL.Query().Get("tenant_id")
	db, store := h.comparisonSource(w, r, id)
	if db == nil {
		return
	}
	defer db.Close()
	// Acquire and establish the source connection before measuring the query.
	conn, err := db.Conn(ctx)
	if err != nil {
		respondError(w, 502, "Source database connection failed")
		return
	}
	defer conn.Close()
	if err = conn.PingContext(ctx); err != nil {
		respondError(w, 502, "Source database connection failed")
		return
	}
	sample := comparisonSample{CheckedAt: time.Now().UTC()}
	sample.Source, sample.SourceMS, err = comparisonRows(ctx, conn, store, from, to)
	if err != nil {
		respondError(w, 502, "Source query failed. Check source connectivity and the reference orders schema.")
		return
	}
	q := url.Values{"tenant_id": {id}, "limit": {"100"}, "sort_by": {"revenue"}}
	if !from.IsZero() {
		q.Set("from", from.Format(time.RFC3339Nano))
		q.Set("to", to.Format(time.RFC3339Nano))
	}
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(h.cfg.AnalyticsURL, "/")+"/v1/analytics/products/top?"+q.Encode(), nil)
	if err != nil {
		respondError(w, 502, "Analytics URL is invalid")
		return
	}
	req.Header.Set("Authorization", "Bearer "+h.cfg.ServiceToken)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		respondError(w, 502, "Analytics service is unavailable")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		respondError(w, 502, fmt.Sprintf("Analytics returned HTTP %d", resp.StatusCode))
		return
	}
	var body struct {
		Tenant string              `json:"tenant_id"`
		Data   []comparisonProduct `json:"data"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&body); err != nil || body.Tenant != id || body.Data == nil {
		respondError(w, 502, "Invalid analytics response or tenant scope")
		return
	}
	sample.AnalyticsMS, err = comparisonDBDuration(resp.Header)
	if err != nil {
		respondError(w, 502, "Analytics DB timing unavailable; rebuild the analytics service")
		return
	}
	sample.Analytics = body.Data
	after, _, err := comparisonRows(ctx, conn, store, from, to)
	if err != nil {
		respondError(w, 502, "Could not recheck source stability")
		return
	}
	sample.Stable = len(comparisonDiff(sample.Source, after)) == 0
	sample.Differences = comparisonDiff(sample.Source, sample.Analytics)
	sample.Match = sample.Stable && len(sample.Differences) == 0
	respondJSON(w, 200, sample)
}

// Tickets bind writes to server-generated fixture IDs, one tenant, and a short
// validity window. There is no endpoint for arbitrary source mutations.
type comparisonTicket struct {
	Tenant  string    `json:"tenant"`
	Prefix  string    `json:"prefix"`
	From    time.Time `json:"from"`
	To      time.Time `json:"to"`
	Expires int64     `json:"expires"`
}

func (h *Handler) signComparison(t comparisonTicket) string {
	b, _ := json.Marshal(t)
	data := base64.RawURLEncoding.EncodeToString(b)
	mac := hmac.New(sha256.New, h.jwtSecret)
	mac.Write([]byte("comparison:" + data))
	return data + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (h *Handler) readComparison(raw, tenant string) (comparisonTicket, error) {
	var t comparisonTicket
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return t, fmt.Errorf("Invalid simulation ticket")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return t, fmt.Errorf("Invalid simulation ticket")
	}
	mac := hmac.New(sha256.New, h.jwtSecret)
	mac.Write([]byte("comparison:" + parts[0]))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return t, fmt.Errorf("Invalid simulation ticket")
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return t, fmt.Errorf("Invalid simulation ticket")
	}
	if json.Unmarshal(b, &t) != nil || t.Tenant != tenant || time.Now().Unix() > t.Expires {
		return t, fmt.Errorf("Simulation ticket expired or belongs to another tenant")
	}
	return t, nil
}

func (h *Handler) ComparisonSimulation(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Tenant string `json:"tenant_id"`
		Ticket string `json:"ticket"`
		Step   int    `json:"step"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.Step < 0 || input.Step > 5 {
		respondError(w, 400, "step must be between 0 and 5")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	db, store := h.comparisonSource(w, r, input.Tenant)
	if db == nil {
		return
	}
	defer db.Close()
	var ticket comparisonTicket
	if input.Step == 0 {
		var ready bool
		err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_publication_tables WHERE pubname='axyra_pub' AND schemaname='public' AND tablename='orders') AND EXISTS(SELECT 1 FROM pg_replication_slots WHERE slot_type='logical' AND database=current_database() AND active)`).Scan(&ready)
		if err != nil || !ready {
			respondError(w, 409, "Source needs orders in axyra_pub and an active logical replication slot before simulation")
			return
		}
		// A fixed window isolates the fixture run from historical rows. Other orders
		// created in this same second are intentionally included on both sides.
		start := time.Now().UTC().Truncate(time.Second)
		ticket = comparisonTicket{Tenant: input.Tenant, Prefix: newID("cmp"), From: start, To: start.Add(time.Second - time.Microsecond), Expires: time.Now().Add(30 * time.Minute).Unix()}
	} else {
		var err error
		ticket, err = h.readComparison(input.Ticket, input.Tenant)
		if err != nil {
			respondError(w, 400, err.Error())
			return
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			respondError(w, 502, "Cannot start source transaction")
			return
		}
		defer tx.Rollback()
		// Serialize retries and refuse skipped creation steps. Retrying a write is safe.
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, ticket.Prefix); err != nil {
			respondError(w, 502, "Cannot lock simulation")
			return
		}
		var count int
		err = tx.QueryRowContext(ctx, `SELECT count(*) FROM public.orders WHERE store_id=$1 AND id IN ($2,$3,$4)`, store, ticket.Prefix+"_1", ticket.Prefix+"_2", ticket.Prefix+"_3").Scan(&count)
		if err != nil {
			respondError(w, 502, "Cannot inspect simulation orders")
			return
		}
		needed := input.Step - 1
		if needed > 3 {
			needed = 3
		}
		if count < needed {
			respondError(w, 409, "Run the preceding creation steps first")
			return
		}
		if input.Step <= 3 {
			amounts := []int{1000, 500, 2000}
			product := ticket.Prefix + "_A"
			if input.Step == 3 {
				product = ticket.Prefix + "_B"
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO public.orders(id,store_id,amount,currency,status,product_id,product_name,category,created_at,updated_at) VALUES($1,$2,$3,'INR','placed',$4,$5,'Comparison',$6,$6) ON CONFLICT(id) DO NOTHING`, fmt.Sprintf("%s_%d", ticket.Prefix, input.Step), store, amounts[input.Step-1], product, "Comparison product "+product[len(product)-1:], ticket.From)
		} else if input.Step == 4 {
			_, err = tx.ExecContext(ctx, `UPDATE public.orders SET amount=750,updated_at=NOW() WHERE store_id=$1 AND id=$2 AND status<>'cancelled' AND amount<>750`, store, ticket.Prefix+"_2")
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE public.orders SET status='cancelled',updated_at=NOW() WHERE store_id=$1 AND id=$2 AND status<>'cancelled'`, store, ticket.Prefix+"_2")
		}
		if err != nil {
			respondError(w, 502, "Source write failed. Check database permissions and schema.")
			return
		}
		if err = tx.Commit(); err != nil {
			respondError(w, 502, "Source commit failed; retry this step with the same ticket")
			return
		}
	}
	respondJSON(w, 200, map[string]any{"ticket": h.signComparison(ticket), "prefix": ticket.Prefix, "from": ticket.From, "to": ticket.To, "step": input.Step})
}
