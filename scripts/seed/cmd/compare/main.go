// compare checks source PostgreSQL results against the public Axyra API.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type product struct {
	ID      string      `json:"product_id"`
	Count   int64       `json:"order_count"`
	Revenue json.Number `json:"revenue"`
}
type result struct {
	Stage     string    `json:"stage"`
	Mode      string    `json:"mode"`
	Pass      bool      `json:"pass"`
	Attempts  int       `json:"attempts"`
	ElapsedMS int64     `json:"elapsed_ms"`
	SourceMS  int64     `json:"source_ms"`
	APIMS     int64     `json:"api_ms"`
	Source    []product `json:"source"`
	Analytics []product `json:"analytics"`
	Error     string    `json:"error,omitempty"`
}
type runner struct {
	db                         *pgxpool.Pool
	client                     *http.Client
	base, token, store, tenant string
	from, to                   time.Time
	timeout, poll              time.Duration
	report                     []result
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func run() error {
	r := &runner{client: &http.Client{Timeout: 15 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
	flag.StringVar(&r.base, "api", "http://localhost:8081", "Control-plane URL")
	flag.StringVar(&r.store, "store", "", "Source orders.store_id")
	flag.StringVar(&r.tenant, "tenant", "", "Canonical Axyra tenant ID")
	flag.DurationVar(&r.timeout, "timeout", 90*time.Second, "Catch-up timeout per comparison")
	flag.DurationVar(&r.poll, "poll", time.Second, "Polling interval")
	from := flag.String("from", "", "Inclusive UTC RFC3339 start (requires -to)")
	to := flag.String("to", "", "Inclusive UTC RFC3339 end (requires -from)")
	simulate := flag.Bool("simulate", false, "Write three fixture orders to an EMPTY test store; retain them afterward")
	output := flag.String("report", "comparison.json", "JSON report destination")
	flag.Parse()
	r.token = os.Getenv("AXYRA_CONSOLE_TOKEN")
	dsn := os.Getenv("SOURCE_DATABASE_URL")
	if r.store == "" || r.tenant == "" || r.token == "" || dsn == "" {
		return fmt.Errorf("set SOURCE_DATABASE_URL and AXYRA_CONSOLE_TOKEN; supply -store and -tenant")
	}
	if r.timeout <= 0 || r.poll <= 0 {
		return fmt.Errorf("timeout and poll must be positive")
	}
	u, err := url.Parse(r.base)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("api must be an HTTP(S) base URL without credentials, query or fragment")
	}
	if (*from == "") != (*to == "") {
		return fmt.Errorf("provide both -from and -to")
	}
	if *simulate && *from != "" {
		return fmt.Errorf("simulation selects its own date window")
	}
	if *from != "" {
		r.from, err = time.Parse(time.RFC3339, *from)
		if err != nil {
			return fmt.Errorf("invalid -from")
		}
		r.to, err = time.Parse(time.RFC3339, *to)
		if err != nil || r.from.After(r.to) {
			return fmt.Errorf("invalid date range")
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	// Never print the connection string or driver connection errors containing it.
	r.db, err = pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("invalid source database configuration")
	}
	defer r.db.Close()
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = r.db.Ping(pingCtx)
	cancel()
	if err != nil {
		return fmt.Errorf("source database connection failed; check SOURCE_DATABASE_URL")
	}
	if *simulate {
		err = r.simulate(ctx)
	} else {
		err = r.check(ctx, "existing data")
	}
	data, marshalErr := json.MarshalIndent(r.report, "", "  ")
	if marshalErr != nil {
		return marshalErr
	}
	if writeErr := os.WriteFile(*output, append(data, '\n'), 0600); writeErr != nil {
		return fmt.Errorf("write report: %w", writeErr)
	}
	fmt.Println("Report:", *output)
	return err
}

func (r *runner) source(ctx context.Context, ranged bool) ([]product, error) {
	query := `SELECT product_id, COUNT(*) FILTER (WHERE status <> 'cancelled'),
	 COALESCE(SUM(amount) FILTER (WHERE status <> 'cancelled'),0)::text
	 FROM public.orders WHERE store_id=$1 AND product_id <> ''`
	args := []any{r.store}
	if ranged {
		query += ` AND created_at >= $2 AND created_at <= $3`
		args = append(args, r.from, r.to)
	}
	query += ` GROUP BY product_id ORDER BY COALESCE(SUM(amount) FILTER (WHERE status <> 'cancelled'),0) DESC, product_id LIMIT 100`
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []product{}
	for rows.Next() {
		var p product
		var amount string
		if err := rows.Scan(&p.ID, &p.Count, &amount); err != nil {
			return nil, err
		}
		p.Revenue = json.Number(amount)
		items = append(items, p)
	}
	return items, rows.Err()
}

func (r *runner) analytics(ctx context.Context, ranged bool) ([]product, error) {
	q := url.Values{"tenant_id": {r.tenant}, "limit": {"100"}, "sort_by": {"revenue"}}
	if ranged {
		q.Set("from", r.from.Format(time.RFC3339Nano))
		q.Set("to", r.to.Format(time.RFC3339Nano))
	}
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(r.base, "/")+"/v1/analytics/products/top?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("analytics HTTP %d", resp.StatusCode)
	}
	var body struct {
		Tenant string    `json:"tenant_id"`
		Data   []product `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	if body.Tenant != r.tenant {
		return nil, fmt.Errorf("unexpected tenant %q", body.Tenant)
	}
	if body.Data == nil {
		return nil, fmt.Errorf("missing products data array")
	}
	return body.Data, nil
}

// Compare money exactly as decimal rationals, avoiding binary floating point tolerances.
func equal(a, b []product) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, ok := new(big.Rat).SetString(a[i].Revenue.String())
		if !ok {
			return false
		}
		y, ok := new(big.Rat).SetString(b[i].Revenue.String())
		if !ok {
			return false
		}
		if a[i].ID != b[i].ID || a[i].Count != b[i].Count || x.Cmp(y) != 0 {
			return false
		}
	}
	return true
}

func (r *runner) check(ctx context.Context, stage string) error {
	modes := []bool{false}
	if !r.from.IsZero() {
		modes = append(modes, true)
	}
	for _, ranged := range modes {
		res := result{Stage: stage, Mode: "all-time"}
		if ranged {
			res.Mode = "date-range"
		}
		start := time.Now()
		checkCtx, cancel := context.WithTimeout(ctx, r.timeout)
		streak := 0
		var previous []product
		for checkCtx.Err() == nil {
			res.Attempts++
			t := time.Now()
			before, err := r.source(checkCtx, ranged)
			res.SourceMS = time.Since(t).Milliseconds()
			if err != nil {
				res.Error = "source query failed: " + err.Error()
				break
			}
			res.Source = before
			t = time.Now()
			res.Analytics, err = r.analytics(checkCtx, ranged)
			res.APIMS = time.Since(t).Milliseconds()
			if err != nil {
				res.Error = err.Error()
				break
			}
			after, err := r.source(checkCtx, ranged)
			if err != nil {
				res.Error = "source recheck failed: " + err.Error()
				break
			}
			if equal(before, after) && equal(before, res.Analytics) {
				if equal(previous, before) {
					streak++
				} else {
					streak = 1
				}
				previous = before
				if streak >= 2 {
					res.Pass = true
					break
				}
			} else {
				streak = 0
				previous = nil
			}
			select {
			case <-checkCtx.Done():
			case <-time.After(r.poll):
			}
		}
		if !res.Pass && res.Error == "" {
			res.Error = "timeout waiting for two stable matching samples"
		}
		cancel()
		res.ElapsedMS = time.Since(start).Milliseconds()
		r.report = append(r.report, res)
		fmt.Printf("%s / %s: pass=%t attempts=%d elapsed=%dms SQL=%dms HTTP=%dms\n", stage, res.Mode, res.Pass, res.Attempts, res.ElapsedMS, res.SourceMS, res.APIMS)
		if !res.Pass {
			return fmt.Errorf("%s / %s: %s (see source and analytics rows in report)", stage, res.Mode, res.Error)
		}
	}
	return nil
}

func (r *runner) simulate(ctx context.Context) error {
	var count int64
	if err := r.db.QueryRow(ctx, `SELECT count(*) FROM public.orders WHERE store_id=$1`, r.store).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return fmt.Errorf("simulation requires an empty dedicated source store")
	}
	var ready bool
	if err := r.db.QueryRow(ctx, `SELECT
	 EXISTS(SELECT 1 FROM pg_publication_tables WHERE pubname='axyra_pub' AND schemaname='public' AND tablename='orders')
	 AND EXISTS(SELECT 1 FROM pg_replication_slots WHERE slot_type='logical' AND database=current_database() AND active)`).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return fmt.Errorf("orders must be published in axyra_pub with an active logical replication slot")
	}
	r.from = time.Now().UTC().Truncate(time.Second)
	r.to = r.from.Add(time.Hour)
	if err := r.check(ctx, "empty baseline"); err != nil {
		return err
	}
	prefix := fmt.Sprintf("compare_%d", time.Now().UnixNano())
	fmt.Println("Fixture prefix:", prefix, "(rows are retained, including on failure)")
	for i, amount := range []int{1000, 500, 2000} {
		pid := prefix + "_A"
		if i == 2 {
			pid = prefix + "_B"
		}
		_, err := r.db.Exec(ctx, `INSERT INTO public.orders
		 (id,store_id,amount,currency,status,product_id,product_name,category,created_at,updated_at)
		 VALUES($1,$2,$3,'INR','placed',$4,$4,'Comparison',$5,$5)`, fmt.Sprintf("%s_%d", prefix, i+1), r.store, amount, pid, r.from)
		if err != nil {
			return err
		}
		if err := r.check(ctx, fmt.Sprintf("create O%d", i+1)); err != nil {
			return err
		}
	}
	for _, step := range []struct{ name, sql string }{
		{"update O2 to 750", `UPDATE public.orders SET amount=750,updated_at=NOW() WHERE id=$1 AND store_id=$2`},
		{"cancel O2", `UPDATE public.orders SET status='cancelled',updated_at=NOW() WHERE id=$1 AND store_id=$2`},
	} {
		tag, err := r.db.Exec(ctx, step.sql, prefix+"_2", r.store)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("fixture missing during %s", step.name)
		}
		if err := r.check(ctx, step.name); err != nil {
			return err
		}
	}
	return nil
}
