package provision

import (
	"context"
	"crypto/cipher"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"time"

	"github.com/axyra/control-plane/internal/auth"
	"github.com/lib/pq"
)

//go:embed orders.sql
var ordersSQL string

type Worker struct {
	DB                                *sql.DB
	Cipher                            cipher.AEAD
	Network, AgentImage, AnalyticsURL string
}
type credentials struct{ Admin, Replication, Writer, Token string }

// WriterConnection is only exposed through the local operator CLI, never HTTP.
func (w *Worker) WriterConnection(ctx context.Context, id string) (string, error) {
	if !tenantPattern.MatchString(id) {
		return "", fmt.Errorf("invalid tenant ID")
	}
	var encrypted []byte
	if err := w.DB.QueryRowContext(ctx, `SELECT encrypted_secrets FROM tenant_provisioning WHERE tenant_id=$1 AND status='ready'`, id).Scan(&encrypted); err != nil {
		return "", fmt.Errorf("managed database is not ready")
	}
	raw, err := unseal(w.Cipher, id, encrypted)
	if err != nil {
		return "", err
	}
	var c credentials
	if err = json.Unmarshal(raw, &c); err != nil {
		return "", err
	}
	if _, err = w.DB.ExecContext(ctx, `INSERT INTO audit_logs(organization_id,actor_id,actor_name,actor_role,action,resource_type,resource_id,details) SELECT organization_id,'local_operator','Local provisioner CLI','operator','tenant.credentials.read','tenant',id,'{}'::jsonb FROM tenants WHERE id=$1`, id); err != nil {
		return "", err
	}
	return dsn("axyra-managed-"+id, "axyra_writer", c.Writer), nil
}

var tenantPattern = regexp.MustCompile(`^tenant_[a-zA-Z0-9]+$`)

func (w *Worker) Run(ctx context.Context) error {
	// A dedicated session lock elects one provisioning worker. The job state is
	// durable; a restart resumes queued/in-progress jobs using the same resources.
	conn, err := w.DB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `SELECT pg_advisory_lock(7243020)`); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock(7243020)`)
	for ctx.Err() == nil {
		var id string
		err = conn.QueryRowContext(ctx, `SELECT tenant_id FROM tenant_provisioning WHERE status IN ('queued','provisioning') ORDER BY created_at LIMIT 1`).Scan(&id)
		if err == sql.ErrNoRows {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
			continue
		}
		if err != nil {
			return err
		}
		jobCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
		err = w.process(jobCtx, id)
		cancel()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			// Use a generic stage-specific message: database errors can include DSNs.
			_, saveErr := conn.ExecContext(ctx, `UPDATE tenant_provisioning SET status='failed',error_message='Provisioning failed during ' || stage || '. An operator should check resource readiness, then retry.',updated_at=NOW() WHERE tenant_id=$1`, id)
			if saveErr != nil {
				return saveErr
			}
			log.Printf("[provisioner] tenant %s failed; resources retained for retry", id)
		}
	}
	return ctx.Err()
}

func (w *Worker) stage(ctx context.Context, id, stage string) error {
	_, err := w.DB.ExecContext(ctx, `UPDATE tenant_provisioning SET status='provisioning',stage=$2,error_message='',updated_at=NOW() WHERE tenant_id=$1`, id, stage)
	return err
}
func (w *Worker) secrets(ctx context.Context, id string) (credentials, error) {
	var encrypted []byte
	var c credentials
	if err := w.DB.QueryRowContext(ctx, `SELECT encrypted_secrets FROM tenant_provisioning WHERE tenant_id=$1`, id).Scan(&encrypted); err != nil {
		return c, err
	}
	if len(encrypted) > 0 {
		plain, err := unseal(w.Cipher, id, encrypted)
		if err != nil {
			return c, err
		}
		err = json.Unmarshal(plain, &c)
		return c, err
	}
	token, _, _, err := auth.NewAgentToken()
	if err != nil {
		return c, err
	}
	c = credentials{Admin: password(), Replication: password(), Writer: password(), Token: token}
	plain, err := json.Marshal(c)
	if err != nil {
		return c, err
	}
	_, err = w.DB.ExecContext(ctx, `UPDATE tenant_provisioning SET encrypted_secrets=$2 WHERE tenant_id=$1`, id, seal(w.Cipher, id, plain))
	return c, err
}
func dsn(host, user, password string) string {
	u := url.URL{Scheme: "postgres", User: url.UserPassword(user, password), Host: host + ":5432", Path: "reference_db", RawQuery: "sslmode=disable&connect_timeout=3"}
	return u.String()
}
func wait(ctx context.Context, ready func() bool) error {
	for {
		if ready() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func (w *Worker) process(ctx context.Context, id string) error {
	if !tenantPattern.MatchString(id) {
		return fmt.Errorf("invalid managed tenant ID")
	}
	var org, external string
	if err := w.DB.QueryRowContext(ctx, `SELECT organization_id,external_tenant_id FROM tenants WHERE id=$1`, id).Scan(&org, &external); err != nil {
		return err
	}
	c, err := w.secrets(ctx, id)
	if err != nil {
		return err
	}
	host := "axyra-managed-" + id
	volume := host + "-data"
	agentID := "agent_" + id
	appID := "app_" + id
	if err = w.stage(ctx, id, "creating_database"); err != nil {
		return err
	}
	exists, err := inspectOwned(ctx, "volume", volume, id)
	if err != nil {
		return err
	}
	if !exists {
		if _, err = docker(ctx, "volume", "create", "--label", "io.axyra.managed=true", "--label", "io.axyra.tenant="+id, volume); err != nil {
			return err
		}
	}
	if err = container(ctx, id, host, "postgres:16", w.Network, []string{"POSTGRES_PASSWORD=" + c.Admin, "POSTGRES_DB=reference_db"}, []string{"--memory", "384m", "--cpus", "0.5", "--pids-limit", "128", "--mount", "type=volume,source=" + volume + ",target=/var/lib/postgresql/data"}, "postgres", "-c", "wal_level=logical", "-c", "max_replication_slots=4", "-c", "max_wal_senders=4"); err != nil {
		return err
	}
	source, err := sql.Open("postgres", dsn(host, "postgres", c.Admin))
	if err != nil {
		return err
	}
	defer source.Close()
	if err = wait(ctx, func() bool { return source.PingContext(ctx) == nil }); err != nil {
		return err
	}
	if err = w.stage(ctx, id, "preparing_schema"); err != nil {
		return err
	}
	tx, err := source.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, ordersSQL); err != nil {
		return err
	}
	for role, pw := range map[string]string{"axyra_ingest": c.Replication, "axyra_writer": c.Writer} {
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)`, role).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			if _, err = tx.ExecContext(ctx, "CREATE ROLE "+pq.QuoteIdentifier(role)+" LOGIN PASSWORD "+pq.QuoteLiteral(pw)); err != nil {
				return err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `ALTER ROLE axyra_ingest REPLICATION; GRANT CONNECT ON DATABASE reference_db TO axyra_ingest,axyra_writer; GRANT USAGE ON SCHEMA public TO axyra_ingest,axyra_writer; GRANT SELECT ON orders TO axyra_ingest; GRANT SELECT,INSERT,UPDATE,DELETE ON orders TO axyra_writer; ALTER TABLE orders REPLICA IDENTITY FULL;`); err != nil {
		return err
	}
	var published bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_publication WHERE pubname='axyra_pub')`).Scan(&published); err != nil {
		return err
	}
	if !published {
		if _, err = tx.ExecContext(ctx, `CREATE PUBLICATION axyra_pub FOR TABLE orders`); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if err = w.stage(ctx, id, "registering_agent"); err != nil {
		return err
	}
	platformTx, err := w.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer platformTx.Rollback()
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO applications(id,organization_id,name) VALUES($1,$2,'Managed database') ON CONFLICT(id) DO NOTHING`, []any{appID, org}},
		{`INSERT INTO agents(id,application_id,name) VALUES($1,$2,$3) ON CONFLICT(id) DO NOTHING`, []any{agentID, appID, "Managed " + external}},
		{`INSERT INTO agent_tokens(id,agent_id,token_hash,token_prefix,name) VALUES($1,$2,$3,$4,'Managed provisioning') ON CONFLICT(id) DO NOTHING`, []any{"tok_" + id, agentID, auth.HashAgentToken(c.Token), auth.TokenPrefix(c.Token)}},
		{`INSERT INTO tenant_mappings(id,agent_id,external_tenant_id,tenant_id) VALUES($1,$2,$3,$4) ON CONFLICT(id) DO NOTHING`, []any{"map_" + id, agentID, external, id}},
		{`UPDATE tenant_provisioning SET agent_id=$2 WHERE tenant_id=$1`, []any{id, agentID}},
	} {
		if _, err = platformTx.ExecContext(ctx, stmt.sql, stmt.args...); err != nil {
			return err
		}
	}
	if err = platformTx.Commit(); err != nil {
		return err
	}
	if err = container(ctx, id, host+"-agent", w.AgentImage, w.Network, []string{"POSTGRES_DSN=" + dsn(host, "axyra_ingest", c.Replication), "SOURCE_TENANT_ID=" + external, "EVENT_ID_NAMESPACE=" + id, "AGENT_ID=" + agentID, "AXYRA_AGENT_TOKEN=" + c.Token, "AXYRA_ENDPOINT=" + w.AnalyticsURL, "REPLICATION_SLOT=axyra_slot_managed"}, []string{"--memory", "128m", "--cpus", "0.25", "--pids-limit", "64", "--read-only", "--tmpfs", "/tmp", "--cap-drop", "ALL", "--security-opt", "no-new-privileges=true"}); err != nil {
		return err
	}
	if err = w.stage(ctx, id, "waiting_for_stream"); err != nil {
		return err
	}
	if err = wait(ctx, func() bool {
		var streaming bool
		err := source.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_replication_slots s JOIN pg_stat_replication r ON r.pid=s.active_pid WHERE s.slot_name='axyra_slot_managed' AND r.state='streaming')`).Scan(&streaming)
		var connected bool
		e := w.DB.QueryRowContext(ctx, `SELECT last_seen_at > NOW()-INTERVAL '1 minute' FROM agents WHERE id=$1`, agentID).Scan(&connected)
		return err == nil && e == nil && streaming && connected
	}); err != nil {
		return err
	}
	done, err := w.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer done.Rollback()
	if _, err = done.ExecContext(ctx, `UPDATE tenants SET status='active' WHERE id=$1 AND status='provisioning'`, id); err != nil {
		return err
	}
	if _, err = done.ExecContext(ctx, `UPDATE tenant_provisioning SET status='ready',stage='ready',error_message='',updated_at=NOW() WHERE tenant_id=$1`, id); err != nil {
		return err
	}
	return done.Commit()
}
