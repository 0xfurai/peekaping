package probe

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"peekaping/internal/dbtx"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
	"github.com/uptrace/bun/driver/pgdriver"
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

	ID              string `bun:"id,pk"`
	ProbeID         string `bun:"probe_id,notnull"`
	Hostname        string `bun:"hostname,notnull"`
	OS              string `bun:"os,notnull"`
	Arch            string `bun:"arch,notnull"`
	Version         string `bun:"version,notnull"`
	ProtocolVersion int    `bun:"protocol_version,notnull"`
	// SupportedTypes is a JSON array of monitor types, in a JSONB column
	SupportedTypes string    `bun:"supported_types,notnull"`
	LastSeenAt     time.Time `bun:"last_seen_at,notnull"`
}

type sqlInstanceConnection struct {
	bun.BaseModel `bun:"table:probe_instance_connections,alias:pic"`

	ID         string     `bun:"id,pk"`
	InstanceID string     `bun:"instance_id,notnull"`
	ProbeID    string     `bun:"probe_id,notnull"`
	StartedAt  time.Time  `bun:"started_at,notnull"`
	LastSeenAt time.Time  `bun:"last_seen_at,notnull"`
	EndedAt    *time.Time `bun:"ended_at"`
}

// decodeTypes reads a column that holds a JSON array of monitor types. A value that isn't one
// reads as no types.
func decodeTypes(column string) []string {
	types := []string{}
	if err := json.Unmarshal([]byte(column), &types); err != nil || types == nil {
		return []string{}
	}
	return types
}

// encodeTypes is the JSON array that a column holds. Nil is an empty array, not null.
func encodeTypes(types []string) (string, error) {
	if types == nil {
		types = []string{}
	}
	encoded, err := json.Marshal(types)
	return string(encoded), err
}

func toDomainInstanceFromSQL(si *sqlInstance) *Instance {
	return &Instance{
		ID:              si.ID,
		Hostname:        si.Hostname,
		OS:              si.OS,
		Arch:            si.Arch,
		Version:         si.Version,
		ProtocolVersion: si.ProtocolVersion,
		SupportedTypes:  decodeTypes(si.SupportedTypes),
		LastSeenAt:      si.LastSeenAt,
	}
}

func toDomainModelFromSQL(sm *sqlModel) *Model {
	return &Model{
		ID:             sm.ID,
		Name:           sm.Name,
		Location:       sm.Location,
		TokenHash:      sm.TokenHash,
		TokenPrefix:    sm.TokenPrefix,
		Disabled:       sm.Disabled,
		Status:         Status(sm.Status),
		SupportedTypes: decodeTypes(sm.SupportedTypes),
		CreatedAt:      sm.CreatedAt,
		UpdatedAt:      sm.UpdatedAt,
	}
}

type sqlStatusChange struct {
	bun.BaseModel `bun:"table:probe_status_changes,alias:psc"`

	ID      string    `bun:"id,pk"`
	ProbeID string    `bun:"probe_id,notnull"`
	Status  string    `bun:"status,notnull"`
	At      time.Time `bun:"at,notnull"`
	// RecordedAt is when the row was stored. It orders the changes that share an At.
	RecordedAt time.Time `bun:"recorded_at,notnull"`
}

func toDomainStatusChange(sc *sqlStatusChange) *StatusChange {
	return &StatusChange{ProbeID: sc.ProbeID, Status: Status(sc.Status), At: sc.At}
}

// SQLRepositoryImpl runs its queries on the transaction ctx carries (see dbtx), or on db
type SQLRepositoryImpl struct {
	db *bun.DB
	// transactor makes the methods that write several rows one transaction: it joins the one ctx
	// carries, or opens its own
	transactor dbtx.Transactor
}

// MARK: Constructor
func NewSQLRepository(db *bun.DB) Repository {
	return &SQLRepositoryImpl{db: db, transactor: dbtx.NewSQLTransactor(db)}
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
	// The probe and the first row of its history are stored together
	err := r.transactor.WithTx(ctx, func(ctx context.Context) error {
		if _, err := dbtx.DB(ctx, r.db).NewInsert().Model(sm).Exec(ctx); err != nil {
			return err
		}
		return r.insertStatusChange(ctx, id, StatusOffline, now)
	})
	if err != nil {
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
	// monitor_probes is the one table that references a probe without cascading the delete
	if isForeignKeyViolation(err) {
		return ErrProbeHasMonitors
	}
	return err
}

// isForeignKeyViolation reports whether the database refused a statement because of a foreign key.
// PostgreSQL says so with a code. SQLite's two drivers word it the same, and neither is a
// dependency of ours.
func isForeignKeyViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr pgdriver.Error
	if errors.As(err, &pgErr) {
		return pgErr.Field('C') == "23503" // foreign_key_violation
	}
	return strings.Contains(err.Error(), "FOREIGN KEY constraint failed")
}

// MARK: CountMonitors
func (r *SQLRepositoryImpl) CountMonitors(ctx context.Context, id string) (int, error) {
	var monitors int
	err := dbtx.DB(ctx, r.db).NewSelect().Table("monitor_probes").ColumnExpr("count(*)").Where("probe_id = ?", id).Scan(ctx, &monitors)
	return monitors, err
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
		instances[i] = toDomainInstanceFromSQL(si)
	}
	return instances, nil
}

// MARK: FindInstance
func (r *SQLRepositoryImpl) FindInstance(ctx context.Context, probeID string, instanceID string) (*Instance, error) {
	si := new(sqlInstance)
	err := dbtx.DB(ctx, r.db).NewSelect().Model(si).Where("id = ?", instanceID).Where("probe_id = ?", probeID).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return toDomainInstanceFromSQL(si), nil
}

// MARK: UpsertInstance
func (r *SQLRepositoryImpl) UpsertInstance(ctx context.Context, probeID string, inst *Instance) error {
	supportedTypes, err := encodeTypes(inst.SupportedTypes)
	if err != nil {
		return err
	}
	// The hello is one of the instance's requests: its connection is written with it
	return r.transactor.WithTx(ctx, func(ctx context.Context) error {
		// The id is the table's primary key, so a conflict on it is the same instance saying hello
		// again. The WHERE keeps another probe's instance out of reach: no row changes then. The
		// statement names the table in full and not by an alias, which PostgreSQL allows in this
		// WHERE only for a table with no alias.
		result, err := dbtx.DB(ctx, r.db).ExecContext(ctx, `INSERT INTO probe_instances (id, probe_id, hostname, os, arch, version, protocol_version, supported_types, last_seen_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
	hostname = EXCLUDED.hostname,
	os = EXCLUDED.os,
	arch = EXCLUDED.arch,
	version = EXCLUDED.version,
	protocol_version = EXCLUDED.protocol_version,
	supported_types = EXCLUDED.supported_types,
	last_seen_at = EXCLUDED.last_seen_at
WHERE probe_instances.probe_id = EXCLUDED.probe_id`,
			inst.ID, probeID, inst.Hostname, inst.OS, inst.Arch, inst.Version, inst.ProtocolVersion, supportedTypes, inst.LastSeenAt.UTC())
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed == 0 {
			return ErrInstanceTaken
		}
		return r.recordConnection(ctx, probeID, inst.ID, inst.LastSeenAt.UTC())
	})
}

// MARK: MakeRoomForInstance
func (r *SQLRepositoryImpl) MakeRoomForInstance(ctx context.Context, probeID string, instanceID string, limit int, quietBefore time.Time) error {
	db := dbtx.DB(ctx, r.db)
	if r.db.Dialect().Name() == dialect.PG {
		// Two hellos at once must not both count 63 rows and each take the last place. The lock
		// on the probe's row makes the second wait for the first to commit. SQLite needs none:
		// its transactions begin immediate, on one connection.
		var locked string
		err := db.NewSelect().Model((*sqlModel)(nil)).Column("id").Where("id = ?", probeID).For("UPDATE").Scan(ctx, &locked)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	held, err := db.NewSelect().Model((*sqlInstance)(nil)).Where("probe_id = ?", probeID).Count(ctx)
	if err != nil {
		return err
	}
	needed := held - limit + 1
	if needed <= 0 {
		return nil
	}
	// An instance the probe holds takes no new row: its hello updates it
	known, err := db.NewSelect().Model((*sqlInstance)(nil)).Where("id = ?", instanceID).Where("probe_id = ?", probeID).Exists(ctx)
	if err != nil || known {
		return err
	}
	// An instance picked can be in contact again before it is deleted (its touch doesn't wait for
	// the probe's lock), so only those still quiet are deleted, and the next oldest quiet ones are
	// picked for the places still missing. When no quiet one is left, the hello is refused: the
	// transaction then rolls back what was deleted and adds nothing.
	for needed > 0 {
		var oldest []string
		err = db.NewSelect().Model((*sqlInstance)(nil)).Column("id").
			Where("probe_id = ?", probeID).
			Where("last_seen_at < ?", quietBefore.UTC()).
			OrderExpr("last_seen_at ASC, id ASC").
			Limit(needed).
			Scan(ctx, &oldest)
		if err != nil {
			return err
		}
		if len(oldest) < needed {
			return ErrTooManyInstances
		}
		result, err := db.NewDelete().Model((*sqlInstance)(nil)).
			Where("id IN (?)", bun.In(oldest)).
			Where("probe_id = ?", probeID).
			Where("last_seen_at < ?", quietBefore.UTC()).
			Exec(ctx)
		if err != nil {
			return err
		}
		deleted, err := result.RowsAffected()
		if err != nil {
			return err
		}
		needed -= int(deleted)
	}
	return nil
}

// MARK: TouchInstance
func (r *SQLRepositoryImpl) TouchInstance(ctx context.Context, probeID string, instanceID string, seenAt time.Time) (bool, error) {
	found := false
	err := r.transactor.WithTx(ctx, func(ctx context.Context) error {
		result, err := dbtx.DB(ctx, r.db).NewUpdate().Model((*sqlInstance)(nil)).
			Set("last_seen_at = ?", seenAt.UTC()).
			Where("id = ?", instanceID).
			Where("probe_id = ?", probeID).
			Exec(ctx)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil || changed == 0 {
			return err
		}
		found = true
		return r.recordConnection(ctx, probeID, instanceID, seenAt.UTC())
	})
	return found && err == nil, err
}

// recordConnection notes a request of the instance at the given time. The instance's open
// connection grows to it when its last request was within InstanceOnlineWindow before it, and
// otherwise a new connection starts. A connection that went quiet is not ended by anything: it
// ended at its last request, which is its last seen time. It runs on the transaction ctx carries.
func (r *SQLRepositoryImpl) recordConnection(ctx context.Context, probeID string, instanceID string, at time.Time) error {
	db := dbtx.DB(ctx, r.db)
	// A request that was slower than a later one takes nothing back from the connection
	result, err := db.NewUpdate().Model((*sqlInstanceConnection)(nil)).
		Set("last_seen_at = CASE WHEN last_seen_at > ? THEN last_seen_at ELSE ? END", at, at).
		Where("instance_id = ?", instanceID).
		Where("ended_at IS NULL").
		Where("last_seen_at >= ?", at.Add(-InstanceOnlineWindow)).
		Exec(ctx)
	if err != nil {
		return err
	}
	extended, err := result.RowsAffected()
	if err != nil || extended > 0 {
		return err
	}
	_, err = db.NewInsert().Model(&sqlInstanceConnection{
		ID:         uuid.New().String(),
		InstanceID: instanceID,
		ProbeID:    probeID,
		StartedAt:  at,
		LastSeenAt: at,
	}).Exec(ctx)
	return err
}

// MARK: RetireInstance
func (r *SQLRepositoryImpl) RetireInstance(ctx context.Context, probeID string, instanceID string, seenAt time.Time, endedAt time.Time) error {
	return r.transactor.WithTx(ctx, func(ctx context.Context) error {
		db := dbtx.DB(ctx, r.db)
		_, err := db.NewUpdate().Model((*sqlInstance)(nil)).
			Set("last_seen_at = ?", seenAt.UTC()).
			Where("id = ?", instanceID).
			Where("probe_id = ?", probeID).
			Exec(ctx)
		if err != nil {
			return err
		}
		// The bye ends the connection the instance's requests have kept open. An instance that
		// was quiet for longer has none to end.
		end := endedAt.UTC()
		_, err = db.NewUpdate().Model((*sqlInstanceConnection)(nil)).
			Set("ended_at = CASE WHEN last_seen_at > ? THEN last_seen_at ELSE ? END", end, end).
			Set("last_seen_at = CASE WHEN last_seen_at > ? THEN last_seen_at ELSE ? END", end, end).
			Where("instance_id = ?", instanceID).
			Where("probe_id = ?", probeID).
			Where("ended_at IS NULL").
			Where("last_seen_at >= ?", end.Add(-InstanceOnlineWindow)).
			Exec(ctx)
		return err
	})
}

// MARK: FindInstanceConnections
func (r *SQLRepositoryImpl) FindInstanceConnections(ctx context.Context, probeID string, since time.Time) ([]*InstanceConnection, error) {
	var scs []*sqlInstanceConnection
	// A bye moves the last seen time to the end, so the last seen time is the later of the two
	err := dbtx.DB(ctx, r.db).NewSelect().Model(&scs).
		Where("probe_id = ?", probeID).
		Where("last_seen_at >= ?", since.UTC()).
		Order("instance_id ASC", "started_at ASC", "id ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	connections := make([]*InstanceConnection, len(scs))
	for i, sc := range scs {
		connections[i] = &InstanceConnection{
			InstanceID: sc.InstanceID,
			ProbeID:    sc.ProbeID,
			StartedAt:  sc.StartedAt,
			LastSeenAt: sc.LastSeenAt,
			EndedAt:    sc.EndedAt,
		}
	}
	return connections, nil
}

// MARK: UpdateStatus
func (r *SQLRepositoryImpl) UpdateStatus(ctx context.Context, id string, from Status, to Status, at time.Time) (bool, error) {
	changed := false
	// The update and the row of the history are one transaction, joined to the caller's when
	// there is one. Of two callers making the same change, the later update finds the status
	// moved already (PostgreSQL makes it wait for the first to commit), changes no row, and
	// records nothing.
	err := r.transactor.WithTx(ctx, func(ctx context.Context) error {
		db := dbtx.DB(ctx, r.db)
		query := db.NewUpdate().Model((*sqlModel)(nil)).
			Set("status = ?", string(to)).
			Set("updated_at = ?", time.Now().UTC()).
			Where("id = ?", id).
			Where("status = ?", string(from))
		if to == StatusOnline {
			// A request that authenticated an enabled probe may be answered after an admin disabled it
			query = query.Where("disabled = ?", false)
		}
		result, err := query.Exec(ctx)
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil || rows == 0 {
			return err
		}
		changed = true

		// The change is not recorded before the one it follows
		latest := new(sqlStatusChange)
		err = db.NewSelect().Model(latest).Where("probe_id = ?", id).
			OrderExpr("at DESC, recorded_at DESC").Limit(1).Scan(ctx)
		switch {
		case err == nil:
			if at.Before(latest.At) {
				at = latest.At
			}
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		return r.insertStatusChange(ctx, id, to, at)
	})
	if err != nil {
		return false, err
	}
	return changed, nil
}

// insertStatusChange adds a row to the probe's history, on the transaction ctx carries
func (r *SQLRepositoryImpl) insertStatusChange(ctx context.Context, probeID string, status Status, at time.Time) error {
	_, err := dbtx.DB(ctx, r.db).NewInsert().Model(&sqlStatusChange{
		ID:         uuid.New().String(),
		ProbeID:    probeID,
		Status:     string(status),
		At:         at.UTC(),
		RecordedAt: time.Now().UTC(),
	}).Exec(ctx)
	return err
}

// MARK: FindStatusChanges
func (r *SQLRepositoryImpl) FindStatusChanges(ctx context.Context, probeID string, since time.Time) ([]*StatusChange, error) {
	db := dbtx.DB(ctx, r.db)
	changes := []*StatusChange{}

	carried := new(sqlStatusChange)
	err := db.NewSelect().Model(carried).Where("probe_id = ?", probeID).Where("at <= ?", since.UTC()).
		OrderExpr("at DESC, recorded_at DESC").Limit(1).Scan(ctx)
	switch {
	case err == nil:
		changes = append(changes, toDomainStatusChange(carried))
	case !errors.Is(err, sql.ErrNoRows):
		return nil, err
	}

	var after []*sqlStatusChange
	err = db.NewSelect().Model(&after).Where("probe_id = ?", probeID).Where("at > ?", since.UTC()).
		OrderExpr("at ASC, recorded_at ASC").Scan(ctx)
	if err != nil {
		return nil, err
	}
	for _, sc := range after {
		changes = append(changes, toDomainStatusChange(sc))
	}
	return changes, nil
}

// MARK: DeleteStatusChangesOlderThan
func (r *SQLRepositoryImpl) DeleteStatusChangesOlderThan(ctx context.Context, before time.Time) (int, error) {
	// A change goes when the probe has a later one before the cutoff: the latest stays
	result, err := dbtx.DB(ctx, r.db).NewDelete().Model((*sqlStatusChange)(nil)).
		Where("at < ?", before.UTC()).
		Where(`EXISTS (SELECT 1 FROM probe_status_changes newer WHERE newer.probe_id = psc.probe_id AND newer.at < ? AND
			(newer.at > psc.at OR (newer.at = psc.at AND (newer.recorded_at > psc.recorded_at OR (newer.recorded_at = psc.recorded_at AND newer.id > psc.id)))))`, before.UTC()).
		Exec(ctx)
	if err != nil {
		return 0, err
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(removed), nil
}

// MARK: DeleteInstancesUnseenSince
func (r *SQLRepositoryImpl) DeleteInstancesUnseenSince(ctx context.Context, before time.Time) (int, error) {
	result, err := dbtx.DB(ctx, r.db).NewDelete().Model((*sqlInstance)(nil)).
		Where("last_seen_at < ?", before.UTC()).
		Exec(ctx)
	if err != nil {
		return 0, err
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(removed), nil
}

// MARK: SetSupportedTypes
func (r *SQLRepositoryImpl) SetSupportedTypes(ctx context.Context, probeID string, types []string) error {
	encoded, err := encodeTypes(types)
	if err != nil {
		return err
	}
	_, err = dbtx.DB(ctx, r.db).NewUpdate().Model((*sqlModel)(nil)).
		Set("supported_types = ?", encoded).
		Where("id = ?", probeID).
		Exec(ctx)
	return err
}
