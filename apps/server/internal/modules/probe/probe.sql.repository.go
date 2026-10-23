package probe

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"peekaping/internal/dbtx"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type sqlModel struct {
	bun.BaseModel `bun:"table:probes,alias:p"`

	ID       string `bun:"id,pk"`
	Name     string `bun:"name,notnull"`
	Location string `bun:"location,notnull"`
	// TokenHash is NULL for a probe with no token, so that two of them don't share a hash
	TokenHash   string `bun:"token_hash,nullzero"`
	TokenPrefix string `bun:"token_prefix,notnull"`
	Disabled    bool   `bun:"disabled,notnull"` // no Bun default: it would replace false with the column default
	Status      string `bun:"status,notnull"`
	// SupportedTypes is a JSON array of monitor types, in a JSONB column
	SupportedTypes string    `bun:"supported_types,notnull"`
	CreatedAt      time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt      time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

type sqlInstance struct {
	bun.BaseModel `bun:"table:probe_instances,alias:pi"`

	ID              string    `bun:"id,pk"`
	ProbeID         string    `bun:"probe_id,notnull"`
	Hostname        string    `bun:"hostname,notnull"`
	OS              string    `bun:"os,notnull"`
	Arch            string    `bun:"arch,notnull"`
	Version         string    `bun:"version,notnull"`
	ProtocolVersion int       `bun:"protocol_version,notnull"`
	LastSeenAt      time.Time `bun:"last_seen_at,notnull"`
}

func toDomainModelFromSQL(sm *sqlModel) *Model {
	supportedTypes := []string{}
	// A value that isn't a JSON array of strings reads as no types
	if err := json.Unmarshal([]byte(sm.SupportedTypes), &supportedTypes); err != nil || supportedTypes == nil {
		supportedTypes = []string{}
	}
	return &Model{
		ID:             sm.ID,
		Name:           sm.Name,
		Location:       sm.Location,
		TokenHash:      sm.TokenHash,
		TokenPrefix:    sm.TokenPrefix,
		Disabled:       sm.Disabled,
		Status:         Status(sm.Status),
		SupportedTypes: supportedTypes,
		CreatedAt:      sm.CreatedAt,
		UpdatedAt:      sm.UpdatedAt,
	}
}

// SQLRepositoryImpl runs its queries on the transaction ctx carries (see dbtx), or on db
type SQLRepositoryImpl struct {
	db *bun.DB
}

// MARK: Constructor
func NewSQLRepository(db *bun.DB) Repository {
	return &SQLRepositoryImpl{db: db}
}

// MARK: Create
func (r *SQLRepositoryImpl) Create(ctx context.Context, probe *CreateModel) (*Model, error) {
	id := probe.ID
	if id == "" {
		id = uuid.New().String()
	}
	now := time.Now().UTC()
	sm := &sqlModel{
		ID:             id,
		Name:           probe.Name,
		Location:       probe.Location,
		TokenHash:      probe.TokenHash,
		TokenPrefix:    probe.TokenPrefix,
		Status:         string(StatusOffline),
		SupportedTypes: "[]",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if _, err := dbtx.DB(ctx, r.db).NewInsert().Model(sm).Exec(ctx); err != nil {
		return nil, err
	}
	// Read the row back, so the caller sees the timestamps as the database stores them: a
	// driver that keeps milliseconds would otherwise answer a create with more precision than
	// every later read
	return r.findOne(ctx, "id = ?", id)
}

// MARK: FindAll
func (r *SQLRepositoryImpl) FindAll(ctx context.Context) ([]*Model, error) {
	var sms []*sqlModel
	// The local probe first, then the others in the order they were added
	err := dbtx.DB(ctx, r.db).NewSelect().Model(&sms).
		OrderExpr("CASE WHEN id = ? THEN 0 ELSE 1 END", LocalID).
		Order("created_at ASC", "id ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	models := make([]*Model, len(sms))
	for i, sm := range sms {
		models[i] = toDomainModelFromSQL(sm)
	}
	return models, nil
}

// MARK: FindByID
func (r *SQLRepositoryImpl) FindByID(ctx context.Context, id string) (*Model, error) {
	return r.findOne(ctx, "id = ?", id)
}

// MARK: FindByTokenHash
func (r *SQLRepositoryImpl) FindByTokenHash(ctx context.Context, tokenHash string) (*Model, error) {
	if tokenHash == "" {
		// A probe with no token has a NULL hash, which no token hash matches
		return nil, nil
	}
	return r.findOne(ctx, "token_hash = ?", tokenHash)
}

func (r *SQLRepositoryImpl) findOne(ctx context.Context, where string, arg any) (*Model, error) {
	sm := new(sqlModel)
	err := dbtx.DB(ctx, r.db).NewSelect().Model(sm).Where(where, arg).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModelFromSQL(sm), nil
}

// MARK: Update
func (r *SQLRepositoryImpl) Update(ctx context.Context, id string, update *UpdateModel) (*Model, error) {
	query := dbtx.DB(ctx, r.db).NewUpdate().Model((*sqlModel)(nil)).Where("id = ?", id).Set("updated_at = ?", time.Now().UTC())
	if update.Name != nil {
		query = query.Set("name = ?", *update.Name)
	}
	if update.Location != nil {
		query = query.Set("location = ?", *update.Location)
	}
	if update.Disabled != nil {
		query = query.Set("disabled = ?", *update.Disabled)
	}
	if _, err := query.Exec(ctx); err != nil {
		return nil, err
	}
	return r.FindByID(ctx, id)
}

// MARK: UpdateToken
func (r *SQLRepositoryImpl) UpdateToken(ctx context.Context, id string, tokenHash string, tokenPrefix string) error {
	_, err := dbtx.DB(ctx, r.db).NewUpdate().Model((*sqlModel)(nil)).
		Set("token_hash = ?", tokenHash).
		Set("token_prefix = ?", tokenPrefix).
		Set("updated_at = ?", time.Now().UTC()).
		Where("id = ?", id).
		Exec(ctx)
	return err
}

// MARK: Delete
func (r *SQLRepositoryImpl) Delete(ctx context.Context, id string) error {
	_, err := dbtx.DB(ctx, r.db).NewDelete().Model((*sqlModel)(nil)).Where("id = ?", id).Exec(ctx)
	return err
}

// MARK: FindInstances
func (r *SQLRepositoryImpl) FindInstances(ctx context.Context, probeID string) ([]*Instance, error) {
	var sis []*sqlInstance
	err := dbtx.DB(ctx, r.db).NewSelect().Model(&sis).
		Where("probe_id = ?", probeID).
		Order("last_seen_at DESC", "id ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	instances := make([]*Instance, len(sis))
	for i, si := range sis {
		instances[i] = &Instance{
			ID:              si.ID,
			Hostname:        si.Hostname,
			OS:              si.OS,
			Arch:            si.Arch,
			Version:         si.Version,
			ProtocolVersion: si.ProtocolVersion,
			LastSeenAt:      si.LastSeenAt,
		}
	}
	return instances, nil
}
