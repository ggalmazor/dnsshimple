package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"dnsshimple/core/identity"
	"dnsshimple/kit"
)

// linkChannel is the NOTIFY channel that wakes parked SSH sessions on any
// instance when a handshake lands. Payload: "<code>:<fingerprint>".
const linkChannel = "dnsshimple_link_completed"

// IdentityRepository is the Postgres implementation of identity.Repository.
type IdentityRepository struct {
	pool *pgxpool.Pool
}

func NewIdentityRepository(pool *pgxpool.Pool) IdentityRepository {
	return IdentityRepository{pool: pool}
}

var _ identity.Repository = IdentityRepository{}

func (repository IdentityRepository) TenantBySSHKey(ctx context.Context, fingerprint string) kit.Outcome[identity.TenantRecord] {
	var record identity.TenantRecord
	err := repository.pool.QueryRow(ctx, `
		SELECT tenants.id, tenants.dnsimple_account_id, tenants.email, tenants.sealed_token
		FROM tenants
		JOIN ssh_keys ON ssh_keys.tenant_id = tenants.id
		WHERE ssh_keys.fingerprint = $1`, fingerprint,
	).Scan(&record.ID, &record.AccountID, &record.Email, &record.SealedToken)
	if errors.Is(err, pgx.ErrNoRows) {
		return kit.Failure[identity.TenantRecord](identity.ErrUnknownFingerprint)
	}
	if err != nil {
		return kit.Failure[identity.TenantRecord](fmt.Errorf("loading the tenant by SSH key: %w", err))
	}
	return kit.Success(record)
}

func (repository IdentityRepository) TenantByID(ctx context.Context, tenantID int64) kit.Outcome[identity.TenantRecord] {
	var record identity.TenantRecord
	err := repository.pool.QueryRow(ctx, `
		SELECT id, dnsimple_account_id, email, sealed_token
		FROM tenants WHERE id = $1`, tenantID,
	).Scan(&record.ID, &record.AccountID, &record.Email, &record.SealedToken)
	if errors.Is(err, pgx.ErrNoRows) {
		return kit.Failure[identity.TenantRecord](identity.ErrUnknownTenant)
	}
	if err != nil {
		return kit.Failure[identity.TenantRecord](fmt.Errorf("loading the tenant by id: %w", err))
	}
	return kit.Success(record)
}

func (repository IdentityRepository) SavePendingLink(ctx context.Context, link identity.PendingLink) kit.Outcome[kit.Nothing] {
	_, err := repository.pool.Exec(ctx, `
		INSERT INTO pending_links (code, fingerprint, started_at)
		VALUES ($1, $2, $3)`,
		link.Code, link.Fingerprint, link.StartedAt)
	if err != nil {
		return kit.Failure[kit.Nothing](fmt.Errorf("saving the pending link: %w", err))
	}
	return kit.Done()
}

func (repository IdentityRepository) PeekPendingLink(ctx context.Context, code string, notBefore time.Time) kit.Outcome[identity.PendingLink] {
	var link identity.PendingLink
	err := repository.pool.QueryRow(ctx, `
		SELECT code, fingerprint, started_at FROM pending_links
		WHERE code = $1 AND started_at > $2`, code, notBefore,
	).Scan(&link.Code, &link.Fingerprint, &link.StartedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return kit.Failure[identity.PendingLink](identity.ErrLinkCodeUnknown)
	}
	if err != nil {
		return kit.Failure[identity.PendingLink](fmt.Errorf("peeking the pending link: %w", err))
	}
	return kit.Success(link)
}

func (repository IdentityRepository) ConsumePendingLink(ctx context.Context, code string, notBefore time.Time) kit.Outcome[identity.PendingLink] {
	// DELETE ... RETURNING makes single-use atomic: whatever races this,
	// exactly one caller gets the row.
	var link identity.PendingLink
	err := repository.pool.QueryRow(ctx, `
		DELETE FROM pending_links
		WHERE code = $1 AND started_at > $2
		RETURNING code, fingerprint, started_at`, code, notBefore,
	).Scan(&link.Code, &link.Fingerprint, &link.StartedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return kit.Failure[identity.PendingLink](identity.ErrLinkCodeUnknown)
	}
	if err != nil {
		return kit.Failure[identity.PendingLink](fmt.Errorf("consuming the pending link: %w", err))
	}
	return kit.Success(link)
}

func (repository IdentityRepository) BindTenant(ctx context.Context, fingerprint string, remote identity.RemoteIdentity, sealedToken []byte) kit.Outcome[identity.Tenant] {
	transaction, err := repository.pool.Begin(ctx)
	if err != nil {
		return kit.Failure[identity.Tenant](fmt.Errorf("opening the bind transaction: %w", err))
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	var tenant identity.Tenant
	err = transaction.QueryRow(ctx, `
		INSERT INTO tenants (dnsimple_account_id, email, sealed_token)
		VALUES ($1, $2, $3)
		ON CONFLICT (dnsimple_account_id)
		DO UPDATE SET email = EXCLUDED.email, sealed_token = EXCLUDED.sealed_token
		RETURNING id, dnsimple_account_id, email`,
		remote.AccountID, remote.Email, sealedToken,
	).Scan(&tenant.ID, &tenant.AccountID, &tenant.Email)
	if err != nil {
		return kit.Failure[identity.Tenant](fmt.Errorf("upserting the tenant: %w", err))
	}

	// A fingerprint moves to the account that last completed a handshake
	// with it; a key cannot belong to two tenants.
	if _, err := transaction.Exec(ctx, `
		INSERT INTO ssh_keys (fingerprint, tenant_id)
		VALUES ($1, $2)
		ON CONFLICT (fingerprint) DO UPDATE SET tenant_id = EXCLUDED.tenant_id`,
		fingerprint, tenant.ID); err != nil {
		return kit.Failure[identity.Tenant](fmt.Errorf("binding the SSH key: %w", err))
	}

	if err := transaction.Commit(ctx); err != nil {
		return kit.Failure[identity.Tenant](fmt.Errorf("committing the bind: %w", err))
	}
	return kit.Success(tenant)
}

func (repository IdentityRepository) AnnounceLink(ctx context.Context, code, fingerprint string) kit.Outcome[kit.Nothing] {
	if _, err := repository.pool.Exec(ctx,
		`SELECT pg_notify($1, $2)`, linkChannel, code+":"+fingerprint); err != nil {
		return kit.Failure[kit.Nothing](fmt.Errorf("announcing the link: %w", err))
	}
	return kit.Done()
}

func (repository IdentityRepository) AwaitLink(ctx context.Context, code, fingerprint string) kit.Outcome[identity.TenantRecord] {
	// A dedicated connection: LISTEN is connection-scoped, and the pool must
	// not hand this connection to anyone else while we wait.
	connection, err := repository.pool.Acquire(ctx)
	if err != nil {
		return kit.Failure[identity.TenantRecord](fmt.Errorf("acquiring the listening connection: %w", err))
	}
	defer connection.Release()

	if _, err := connection.Exec(ctx, "LISTEN "+linkChannel); err != nil {
		return kit.Failure[identity.TenantRecord](fmt.Errorf("listening on %s: %w", linkChannel, err))
	}

	// The handshake may have landed before we started listening; check once
	// after LISTEN so nothing can fall between the two.
	if already := repository.TenantBySSHKey(ctx, fingerprint); !already.Failed() {
		return already
	}

	for {
		notification, err := connection.Conn().WaitForNotification(ctx)
		if err != nil {
			return kit.Failure[identity.TenantRecord](fmt.Errorf("waiting for the link announcement: %w", err))
		}
		announcedCode, announcedFingerprint, _ := strings.Cut(notification.Payload, ":")
		if announcedCode == code || announcedFingerprint == fingerprint {
			return repository.TenantBySSHKey(ctx, fingerprint)
		}
	}
}
