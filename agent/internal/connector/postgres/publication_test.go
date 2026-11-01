package postgres

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"os"
	"testing"
	"time"
)

func TestPublicationWithRestrictedRole(t *testing.T) {
	dsn := os.Getenv("TEST_PUBLICATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_PUBLICATION_DATABASE_URL to disposable PostgreSQL")
	}
	ctx := context.Background()
	conn, err := pgconn.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	table, role, pub := "pubtest_"+suffix, "repltest_"+suffix, "publication_"+suffix
	exec := func(q string) {
		t.Helper()
		if _, err := conn.Exec(ctx, q).ReadAll(); err != nil {
			t.Fatal(err)
		}
	}
	exec("CREATE TABLE public." + table + "(id text PRIMARY KEY); CREATE ROLE " + role + "; GRANT USAGE ON SCHEMA public TO " + role + "; GRANT SELECT ON public." + table + " TO " + role + "; CREATE PUBLICATION " + pub + " FOR TABLE public." + table)
	defer func() {
		conn.Exec(ctx, "RESET ROLE; DROP PUBLICATION "+pub+"; DROP TABLE public."+table+"; DROP OWNED BY "+role+"; DROP ROLE "+role).ReadAll()
	}()
	exec("SET ROLE " + role)
	connector := New(dsn, "unused", pub, []string{table})
	if err := connector.ensurePublication(ctx, conn); err != nil {
		t.Fatalf("restricted role cannot inspect its published table: %v", err)
	}
}
