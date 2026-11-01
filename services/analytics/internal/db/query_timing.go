package db

import (
	"context"
	"database/sql"
	"sync/atomic"
	"time"
)

type queryTimingKey struct{}

// QueryTiming sums time inside database/sql calls, excluding work between calls.
type QueryTiming struct {
	nanos atomic.Int64
	count atomic.Int64
}

func WithQueryTiming(ctx context.Context) (context.Context, *QueryTiming) {
	t := &QueryTiming{}
	return context.WithValue(ctx, queryTimingKey{}, t), t
}

func (t *QueryTiming) Snapshot() (float64, int64) {
	return float64(t.nanos.Load()) / float64(time.Millisecond), t.count.Load()
}

func (t *QueryTiming) add(start time.Time) {
	if t != nil {
		t.nanos.Add(int64(time.Since(start)))
	}
}

type TimedRow struct {
	*sql.Row
	timing *QueryTiming
}

func (r *TimedRow) Scan(dest ...any) error {
	start := time.Now()
	defer r.timing.add(start)
	return r.Row.Scan(dest...)
}

func (d *DB) QueryRowContext(ctx context.Context, query string, args ...any) *TimedRow {
	t, _ := ctx.Value(queryTimingKey{}).(*QueryTiming)
	start := time.Now()
	row := d.DB.QueryRowContext(ctx, query, args...)
	t.add(start)
	if t != nil {
		t.count.Add(1)
	}
	return &TimedRow{Row: row, timing: t}
}

type TimedRows struct {
	*sql.Rows
	timing *QueryTiming
}

func (r *TimedRows) Next() bool {
	start := time.Now()
	defer r.timing.add(start)
	return r.Rows.Next()
}

func (r *TimedRows) Scan(dest ...any) error {
	start := time.Now()
	defer r.timing.add(start)
	return r.Rows.Scan(dest...)
}

func (r *TimedRows) Close() error {
	start := time.Now()
	defer r.timing.add(start)
	return r.Rows.Close()
}

func (d *DB) QueryContext(ctx context.Context, query string, args ...any) (*TimedRows, error) {
	t, _ := ctx.Value(queryTimingKey{}).(*QueryTiming)
	start := time.Now()
	rows, err := d.DB.QueryContext(ctx, query, args...)
	t.add(start)
	if t != nil {
		t.count.Add(1)
	}
	if err != nil {
		return nil, err
	}
	return &TimedRows{Rows: rows, timing: t}, nil
}
