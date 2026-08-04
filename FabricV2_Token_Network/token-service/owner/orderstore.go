package main

import (
	"context"
	"database/sql"
	"time"

	"github.com/hyperledger-labs/fabric-smart-client/platform/view/services"
	"github.com/hyperledger-labs/fabric-smart-client/platform/view/services/config"
	"github.com/pkg/errors"

	// pgx is already in this module's build list (the Token SDK uses it for
	// its own Postgres persistence), so this adds a driver registration, not
	// a dependency.
	_ "github.com/jackc/pgx/v5/stdlib"
)

// The ledger records the individual transfers but has no concept of an order,
// so this state machine lives here. confirmed and cancelled are terminal.
//
//	(none) --/lock--> locked --/confirm--> confirmed
//	                         \--/cancel--> cancelled
const (
	OrderStatusLocked    = "locked"
	OrderStatusConfirmed = "confirmed"
	OrderStatusCancelled = "cancelled"
)

type OrderLock struct {
	OrderID   string
	TokenType string
	Quantity  uint64
	Sender    string
	ListingID string
	Status    string
	LockTxID  string
	FinalTxID string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// See TokenService.orders for why this is a table rather than a ledger query.
type orderStore struct {
	db *sql.DB
}

// newOrderStore opens the same Postgres database the node already uses,
// reading the DSN from the node's own config rather than duplicating it in a
// second env var. "default" matches the vault/kvs/token stores.
func newOrderStore(sp services.Provider) (*orderStore, error) {
	dsn := config.GetProvider(sp).GetString("fsc.persistences.default.opts.dataSource")
	if dsn == "" {
		return nil, errors.New("fsc.persistences.default.opts.dataSource is empty; cannot open order store")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, errors.Wrap(err, "failed opening order store")
	}
	// Small pool: three low-frequency endpoints that must not compete with the
	// SDK's own pool (maxOpenConns 25) for the server's connection budget.
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxIdleTime(time.Minute)

	s := &orderStore{db: db}
	if err := s.init(context.Background()); err != nil {
		_ = db.Close()

		return nil, err
	}

	return s, nil
}

func (s *orderStore) init(ctx context.Context) error {
	// app_ prefix keeps this clear of the SDK-managed tables in this database.
	// order_id is the PRIMARY KEY, which is what makes /lock idempotent.
	const ddl = `
CREATE TABLE IF NOT EXISTS app_order_locks (
    order_id    TEXT PRIMARY KEY,
    token_type  TEXT        NOT NULL,
    quantity    BIGINT      NOT NULL,
    sender      TEXT        NOT NULL,
    listing_id  TEXT        NOT NULL DEFAULT '',
    status      TEXT        NOT NULL,
    lock_tx_id  TEXT        NOT NULL DEFAULT '',
    final_tx_id TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`
	if _, err := s.db.ExecContext(ctx, ddl); err != nil {
		return errors.Wrap(err, "failed creating app_order_locks table")
	}

	return nil
}

func (s *orderStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}

	return s.db.Close()
}

// Claim inserts a new order in the `locked` state, returning errOrderExists if
// the orderId was ever used before - including for an already-settled order,
// since IDs are never reused.
func (s *orderStore) Claim(ctx context.Context, o OrderLock) error {
	res, err := s.db.ExecContext(ctx, `
INSERT INTO app_order_locks (order_id, token_type, quantity, sender, listing_id, status)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (order_id) DO NOTHING`,
		o.OrderID, o.TokenType, int64(o.Quantity), o.Sender, o.ListingID, OrderStatusLocked)
	if err != nil {
		return errors.Wrap(err, "failed claiming order")
	}
	n, err := res.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed reading claim result")
	}
	if n == 0 {
		return errOrderExists
	}

	return nil
}

func (s *orderStore) Get(ctx context.Context, orderID string) (OrderLock, error) {
	var o OrderLock
	var q int64
	err := s.db.QueryRowContext(ctx, `
SELECT order_id, token_type, quantity, sender, listing_id, status, lock_tx_id, final_tx_id, created_at, updated_at
FROM app_order_locks WHERE order_id = $1`, orderID).
		Scan(&o.OrderID, &o.TokenType, &q, &o.Sender, &o.ListingID, &o.Status,
			&o.LockTxID, &o.FinalTxID, &o.CreatedAt, &o.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return o, errOrderNotFound
	}
	if err != nil {
		return o, errors.Wrap(err, "failed reading order")
	}
	o.Quantity = uint64(q)

	return o, nil
}

func (s *orderStore) SetLockTxID(ctx context.Context, orderID, txID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE app_order_locks SET lock_tx_id = $2, updated_at = NOW() WHERE order_id = $1`,
		orderID, txID)

	return errors.Wrap(err, "failed recording lock txid")
}

// Release moves a locked order to a terminal state. The `AND status = 'locked'`
// guard is what makes /confirm and /cancel single-shot: a second attempt
// updates zero rows instead of moving the tokens twice.
func (s *orderStore) Release(ctx context.Context, orderID, newStatus, finalTxID string) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE app_order_locks
SET status = $2, final_tx_id = $3, updated_at = NOW()
WHERE order_id = $1 AND status = $4`,
		orderID, newStatus, finalTxID, OrderStatusLocked)
	if err != nil {
		return errors.Wrap(err, "failed releasing order")
	}
	n, err := res.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed reading release result")
	}
	if n == 0 {
		return errOrderNotLocked
	}

	return nil
}

// Separate from Release because the status flip must happen BEFORE the
// transfer to win races, while the txid only exists after it.
func (s *orderStore) setFinalTxID(ctx context.Context, orderID, txID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE app_order_locks SET final_tx_id = $2, updated_at = NOW() WHERE order_id = $1`,
		orderID, txID)

	return errors.Wrap(err, "failed recording final txid")
}

// Abandon removes a claim whose on-chain lock transfer failed. Without it the
// orderId would sit in `locked` with nothing escrowed and could never be retried.
func (s *orderStore) Abandon(ctx context.Context, orderID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM app_order_locks WHERE order_id = $1 AND status = $2 AND lock_tx_id = ''`,
		orderID, OrderStatusLocked)

	return errors.Wrap(err, "failed abandoning order")
}
