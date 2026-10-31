package postgres

import (
	"context"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/axyra/agent/internal/connector"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
)

type PostgresConnector struct {
	dsn             string
	slotName        string
	publicationName string
	tables          []string
	// startLSN is the LSN to resume replication from. When empty the connector
	// starts from the current WAL tip (first run). On subsequent runs the agent
	// populates this from the last confirmed_lsn returned by GET /v1/agent/ack.
	startLSN  string
	conn      *pgconn.PgConn
	relations map[uint32]*pglogrepl.RelationMessage
	mu        sync.Mutex
	closed    bool
}

func New(dsn, slotName, publicationName string, tables []string) *PostgresConnector {
	if slotName == "" {
		slotName = "axyra_slot"
	}
	if publicationName == "" {
		publicationName = "axyra_pub"
	}
	return &PostgresConnector{
		dsn:             dsn,
		slotName:        slotName,
		publicationName: publicationName,
		tables:          tables,
		relations:       make(map[uint32]*pglogrepl.RelationMessage),
	}
}

// WithStartLSN sets the LSN from which replication should resume. "0/0" and ""
// both mean "start from the current WAL tip".
func (c *PostgresConnector) WithStartLSN(lsn string) *PostgresConnector {
	if lsn != "" && lsn != "0/0" {
		c.startLSN = lsn
	}
	return c
}

func (c *PostgresConnector) Start(ctx context.Context, out chan<- connector.ChangeEvent) error {
	log.Printf("[agent-postgres] starting postgres logical replication connector (slot: %s, pub: %s)", c.slotName, c.publicationName)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := c.runReplication(ctx, out)
		if err != nil {
			c.mu.Lock()
			if c.closed {
				c.mu.Unlock()
				return nil
			}
			c.mu.Unlock()

			log.Printf("[agent-postgres] replication interrupted: %v. Reconnecting in 3s...", err)
			select {
			case <-time.After(3 * time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

func (c *PostgresConnector) runReplication(ctx context.Context, out chan<- connector.ChangeEvent) error {
	// 1. Establish replication connection
	dsn := c.dsn
	if !strings.Contains(dsn, "sslmode=") {
		if strings.Contains(dsn, "?") {
			dsn += "&sslmode=disable"
		} else {
			dsn += "?sslmode=disable"
		}
	}
	connCfg, err := pgconn.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("parse dsn: %w", err)
	}

	// Normal connection first to ensure publication and tables exist
	normalConn, err := pgconn.ConnectConfig(ctx, connCfg)
	if err != nil {
		return fmt.Errorf("connect normal: %w", err)
	}
	defer normalConn.Close(context.Background())

	c.ensurePublication(ctx, normalConn)

	// Now configure replication mode
	if connCfg.RuntimeParams == nil {
		connCfg.RuntimeParams = make(map[string]string)
	}
	connCfg.RuntimeParams["replication"] = "database"

	conn, err := pgconn.ConnectConfig(ctx, connCfg)
	if err != nil {
		return fmt.Errorf("connect replication: %w", err)
	}
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
	defer func() {
		_ = conn.Close(context.Background())
	}()

	// 2. Identify system
	sysident, err := pglogrepl.IdentifySystem(ctx, conn)
	if err != nil {
		return fmt.Errorf("identify system: %w", err)
	}
	log.Printf("[agent-postgres] system identified: systemid=%s timeline=%d xlogpos=%s db=%s",
		sysident.SystemID, sysident.Timeline, sysident.XLogPos, sysident.DBName)

	// 3. Create replication slot if it doesn't already exist
	_, err = pglogrepl.CreateReplicationSlot(ctx, conn, c.slotName, "pgoutput", pglogrepl.CreateReplicationSlotOptions{
		Temporary: false,
	})
	if err != nil {
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "42710" {
			log.Printf("[agent-postgres] replication slot %s already exists", c.slotName)
		} else {
			return fmt.Errorf("create replication slot: %w", err)
		}
	} else {
		log.Printf("[agent-postgres] created replication slot %s", c.slotName)
	}

	// 4. Start replication
	// Prefer the persisted cursor so we replay any events that were queued but
	// not yet committed after the last agent restart.
	startLSN := sysident.XLogPos
	if c.startLSN != "" {
		if parsed, err2 := pglogrepl.ParseLSN(c.startLSN); err2 == nil {
			startLSN = parsed
			log.Printf("[agent-postgres] resuming replication from persisted LSN %s", c.startLSN)
		} else {
			log.Printf("[agent-postgres] could not parse persisted LSN %q, starting from current tip: %v", c.startLSN, err2)
		}
	}
	pluginArgs := []string{
		"proto_version '1'",
		fmt.Sprintf("publication_names '%s'", c.publicationName),
		"messages 'true'",
	}

	log.Printf("[agent-postgres] starting replication from LSN %s", startLSN)
	err = pglogrepl.StartReplication(ctx, conn, c.slotName, startLSN, pglogrepl.StartReplicationOptions{
		PluginArgs: pluginArgs,
	})
	if err != nil {
		return fmt.Errorf("start replication: %w", err)
	}

	lastStatusUpdate := time.Now()
	var currentLSN pglogrepl.LSN = startLSN

	// 5. Message loop
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if time.Since(lastStatusUpdate) > 10*time.Second {
			_ = pglogrepl.SendStandbyStatusUpdate(ctx, conn, pglogrepl.StandbyStatusUpdate{
				WALWritePosition: currentLSN,
				WALFlushPosition: currentLSN,
				WALApplyPosition: currentLSN,
				ClientTime:       time.Now(),
				ReplyRequested:   false,
			})
			lastStatusUpdate = time.Now()
		}

		msgCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		backendMsg, err := conn.ReceiveMessage(msgCtx)
		cancel()

		if err != nil {
			if pgconn.Timeout(err) {
				continue
			}
			return fmt.Errorf("receive message: %w", err)
		}

		switch msg := backendMsg.(type) {
		case *pgproto3.CopyData:
			if len(msg.Data) == 0 {
				continue
			}
			switch msg.Data[0] {
			case pglogrepl.PrimaryKeepaliveMessageByteID:
				pkm, err := pglogrepl.ParsePrimaryKeepaliveMessage(msg.Data[1:])
				if err != nil {
					log.Printf("[agent-postgres] parse keepalive error: %v", err)
					continue
				}
				if pkm.ServerWALEnd > currentLSN {
					currentLSN = pkm.ServerWALEnd
				}
				if pkm.ReplyRequested {
					_ = pglogrepl.SendStandbyStatusUpdate(ctx, conn, pglogrepl.StandbyStatusUpdate{
						WALWritePosition: currentLSN,
						WALFlushPosition: currentLSN,
						WALApplyPosition: currentLSN,
						ClientTime:       time.Now(),
						ReplyRequested:   false,
					})
					lastStatusUpdate = time.Now()
				}

			case pglogrepl.XLogDataByteID:
				xld, err := pglogrepl.ParseXLogData(msg.Data[1:])
				if err != nil {
					log.Printf("[agent-postgres] parse xlog data error: %v", err)
					continue
				}
				if xld.ServerWALEnd > currentLSN {
					currentLSN = xld.ServerWALEnd
				}

				logicalMsg, err := pglogrepl.Parse(xld.WALData)
				if err != nil {
					log.Printf("[agent-postgres] parse logical replication msg error: %v", err)
					continue
				}

				c.handleLogicalMessage(logicalMsg, xld.WALStart.String(), out)
			}
		}
	}
}

func (c *PostgresConnector) handleLogicalMessage(m pglogrepl.Message, lsn string, out chan<- connector.ChangeEvent) {
	switch lm := m.(type) {
	case *pglogrepl.RelationMessage:
		c.mu.Lock()
		c.relations[lm.RelationID] = lm
		c.mu.Unlock()
		log.Printf("[agent-postgres] cached schema for relation: %s (id: %d, cols: %d)", lm.RelationName, lm.RelationID, lm.ColumnNum)

	case *pglogrepl.InsertMessage:
		c.mu.Lock()
		rel := c.relations[lm.RelationID]
		c.mu.Unlock()
		if rel == nil {
			return
		}
		row := decodeTuple(rel, lm.Tuple)
		log.Printf("[agent-postgres] captured WAL INSERT on %s (LSN: %s)", rel.RelationName, lsn)
		out <- connector.ChangeEvent{
			Table:     rel.RelationName,
			Operation: "INSERT",
			LSN:       lsn,
			Timestamp: time.Now().UTC(),
			Row:       row,
		}

	case *pglogrepl.UpdateMessage:
		c.mu.Lock()
		rel := c.relations[lm.RelationID]
		c.mu.Unlock()
		if rel == nil {
			return
		}
		row := decodeTuple(rel, lm.NewTuple)
		var oldRow map[string]interface{}
		if lm.OldTuple != nil {
			oldRow = decodeTuple(rel, lm.OldTuple)
		}
		log.Printf("[agent-postgres] captured WAL UPDATE on %s (LSN: %s)", rel.RelationName, lsn)
		out <- connector.ChangeEvent{
			Table:     rel.RelationName,
			Operation: "UPDATE",
			LSN:       lsn,
			Timestamp: time.Now().UTC(),
			Row:       row,
			OldRow:    oldRow,
		}

	case *pglogrepl.DeleteMessage:
		c.mu.Lock()
		rel := c.relations[lm.RelationID]
		c.mu.Unlock()
		if rel == nil {
			return
		}
		var oldRow map[string]interface{}
		if lm.OldTuple != nil {
			oldRow = decodeTuple(rel, lm.OldTuple)
		}
		log.Printf("[agent-postgres] captured WAL DELETE on %s (LSN: %s)", rel.RelationName, lsn)
		out <- connector.ChangeEvent{
			Table:     rel.RelationName,
			Operation: "DELETE",
			LSN:       lsn,
			Timestamp: time.Now().UTC(),
			OldRow:    oldRow,
		}
	}
}

func (c *PostgresConnector) ensurePublication(ctx context.Context, conn *pgconn.PgConn) {
	// Try creating publication for configured tables or create empty and add
	var tableList string
	if len(c.tables) > 0 {
		tableList = strings.Join(c.tables, ", ")
	} else {
		tableList = "orders"
	}

	query := fmt.Sprintf(`
		DO $$
		BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_publication WHERE pubname = '%s') THEN
				BEGIN
					CREATE PUBLICATION %s FOR TABLE %s;
				EXCEPTION WHEN undefined_table THEN
					CREATE PUBLICATION %s;
				END;
			ELSE
				BEGIN
					ALTER PUBLICATION %s ADD TABLE %s;
				EXCEPTION WHEN OTHERS THEN
					NULL;
				END;
			END IF;
		END
		$$;
	`, c.publicationName, c.publicationName, tableList, c.publicationName, c.publicationName, tableList)

	res := conn.Exec(ctx, query)
	_, _ = res.ReadAll()
}

func decodeTuple(rel *pglogrepl.RelationMessage, tuple *pglogrepl.TupleData) map[string]interface{} {
	row := make(map[string]interface{})
	for idx, col := range tuple.Columns {
		if idx >= len(rel.Columns) {
			break
		}
		rc := rel.Columns[idx]
		switch col.DataType {
		case 'n': // null
			row[rc.Name] = nil
		case 't': // text
			row[rc.Name] = decodeValue(rc.DataType, string(col.Data))
		case 'u':
			// An unchanged TOASTed value: its contents are not in the stream.
			// Leave the key out so downstream sees "not provided", not "empty".
		default:
			row[rc.Name] = string(col.Data)
		}
	}
	return row
}

// PostgreSQL type OIDs (pg_type.oid) that decode to a non-string JSON value.
const (
	oidBool    = 16
	oidInt8    = 20
	oidInt2    = 21
	oidInt4    = 23
	oidOID     = 26
	oidFloat4  = 700
	oidFloat8  = 701
	oidNumeric = 1700
)

// decodeValue converts a column's text representation using the column's
// declared type. Only booleans, integers and decimals become JSON numbers or
// booleans; everything else, including text columns that merely look numeric,
// stays a string. Guessing from the text instead would turn a SKU of "007" into
// the number 7, "1e5" into 100000 and "t" into true.
func decodeValue(typeOID uint32, s string) interface{} {
	switch typeOID {
	case oidBool:
		return s == "t" || s == "true"
	case oidInt2, oidInt4, oidInt8, oidOID:
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			return i
		}
	case oidFloat4, oidFloat8, oidNumeric:
		// NaN and Infinity cannot be encoded in JSON, so they stay text.
		if f, err := strconv.ParseFloat(s, 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
			return f
		}
	}
	return s
}

func (c *PostgresConnector) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	if c.conn != nil {
		return c.conn.Close(context.Background())
	}
	return nil
}
