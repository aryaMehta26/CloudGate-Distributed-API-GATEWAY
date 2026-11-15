// Package cassandra provides a thin wrapper around gocql for CloudGate.
package cassandra

import (
	"fmt"
	"time"

	"github.com/gocql/gocql"

	"github.com/aryaMehta26/cloudgate/internal/config"
)

// Client wraps a gocql session.
type Client struct {
	session  *gocql.Session
	keyspace string
}

// New creates a Cassandra client and applies the schema if needed.
// Cluster timeouts and retry policy.
const (
	queryTimeout   = 15 * time.Second
	connectTimeout = 10 * time.Second
	numRetries     = 3
	retryFloor     = 100 * time.Millisecond
	retryCeiling   = 2 * time.Second
)

func New(cfg config.CassandraConfig) (*Client, error) {
	cluster := gocql.NewCluster(cfg.Hosts...)
	cluster.Keyspace = cfg.Keyspace
	cluster.Consistency = gocql.LocalQuorum
	cluster.Timeout = queryTimeout
	cluster.ConnectTimeout = connectTimeout
	cluster.RetryPolicy = &gocql.ExponentialBackoffRetryPolicy{
		NumRetries: numRetries,
		Min:        retryFloor,
		Max:        retryCeiling,
	}
	cluster.PoolConfig.HostSelectionPolicy = gocql.TokenAwareHostPolicy(gocql.RoundRobinHostPolicy())

	if cfg.Username != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{
			Username: cfg.Username,
			Password: cfg.Password,
		}
	}

	session, err := cluster.CreateSession()
	if err != nil {
		return nil, fmt.Errorf("create cassandra session: %w", err)
	}

	c := &Client{session: session, keyspace: cfg.Keyspace}
	if err := c.applySchema(); err != nil {
		session.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	return c, nil
}

// Session returns the underlying gocql session.
func (c *Client) Session() *gocql.Session {
	return c.session
}

// Keyspace returns the configured keyspace name.
func (c *Client) Keyspace() string {
	return c.keyspace
}

// Close shuts down the Cassandra session.
func (c *Client) Close() {
	c.session.Close()
}

// applySchema creates the keyspace and tables if they do not exist.
func (c *Client) applySchema() error {
	stmts := []string{
		fmt.Sprintf(`
			CREATE KEYSPACE IF NOT EXISTS %s
			WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1}
		`, c.keyspace),
		fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS %s.request_audit (
				tenant_id   TEXT,
				request_id  UUID,
				ts          TIMESTAMP,
				method      TEXT,
				path        TEXT,
				status      INT,
				latency_ms  BIGINT,
				client_ip   TEXT,
				PRIMARY KEY ((tenant_id), ts, request_id)
			) WITH CLUSTERING ORDER BY (ts DESC)
			  AND default_time_to_live = 2592000
		`, c.keyspace),
	}

	for _, stmt := range stmts {
		if err := c.session.Query(stmt).Exec(); err != nil {
			return fmt.Errorf("schema stmt failed: %w\nstmt: %s", err, stmt)
		}
	}

	return nil
}
