package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"testing"
	"time"
)

type timingConnector struct{}

func (timingConnector) Connect(context.Context) (driver.Conn, error) { return timingConn{}, nil }
func (timingConnector) Driver() driver.Driver                        { return timingDriver{} }

type timingDriver struct{}

func (timingDriver) Open(string) (driver.Conn, error) { return timingConn{}, nil }

type timingConn struct{}

func (timingConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unused") }
func (timingConn) Close() error                        { return nil }
func (timingConn) Begin() (driver.Tx, error)           { return nil, errors.New("unused") }
func (timingConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	time.Sleep(5 * time.Millisecond)
	return &timingRows{}, nil
}

type timingRows struct{ read bool }

func (*timingRows) Columns() []string { return []string{"value"} }
func (*timingRows) Close() error      { return nil }
func (r *timingRows) Next(dest []driver.Value) error {
	if r.read {
		return io.EOF
	}
	time.Sleep(5 * time.Millisecond)
	r.read = true
	dest[0] = int64(1)
	return nil
}

type slowScanner struct{}

func (*slowScanner) Scan(any) error { time.Sleep(5 * time.Millisecond); return nil }

func TestQueryTimingIncludesExecuteFetchScanButExcludesApplicationWork(t *testing.T) {
	d := &DB{DB: sql.OpenDB(timingConnector{})}
	defer d.Close()
	for _, single := range []bool{true, false} {
		ctx, timing := WithQueryTiming(context.Background())
		start := time.Now()
		time.Sleep(15 * time.Millisecond)
		var target slowScanner
		if single {
			row := d.QueryRowContext(ctx, "test")
			time.Sleep(15 * time.Millisecond)
			if err := row.Scan(&target); err != nil {
				t.Fatal(err)
			}
		} else {
			rows, err := d.QueryContext(ctx, "test")
			if err != nil {
				t.Fatal(err)
			}
			time.Sleep(15 * time.Millisecond)
			if !rows.Next() {
				t.Fatal("missing row")
			}
			if err := rows.Scan(&target); err != nil {
				t.Fatal(err)
			}
			if rows.Next() {
				t.Fatal("unexpected row")
			}
			if err := rows.Close(); err != nil {
				t.Fatal(err)
			}
		}
		ms, count := timing.Snapshot()
		if ms < 15 || count != 1 {
			t.Fatalf("incomplete DB timing: %f ms, %d queries", ms, count)
		}
		if float64(time.Since(start))/float64(time.Millisecond)-ms < 30 {
			t.Fatal("application work included")
		}
	}
	var n int
	if err := d.QueryRowContext(context.Background(), "untimed").Scan(&n); err != nil || n != 1 {
		t.Fatalf("untimed callers changed: %v, %d", err, n)
	}
}
