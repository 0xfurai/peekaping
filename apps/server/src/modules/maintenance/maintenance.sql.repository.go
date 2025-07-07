package maintenance

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type sqlModel struct {
	bun.BaseModel `bun:"table:maintenances,alias:m"`

	ID            string    `bun:"id,pk"`
	Title         string    `bun:"title,notnull"`
	Description   string    `bun:"description"`
	Active        bool      `bun:"active,notnull,default:true"`
	Strategy      string    `bun:"strategy,notnull"`
	StartDateTime *string   `bun:"start_date_time"`
	EndDateTime   *string   `bun:"end_date_time"`
	StartTime     *string   `bun:"start_time"`
	EndTime       *string   `bun:"end_time"`
	Weekdays      string    `bun:"weekdays"`      // Store as JSON string for compatibility
	DaysOfMonth   string    `bun:"days_of_month"` // Store as JSON string for compatibility
	IntervalDay   *int      `bun:"interval_day"`
	Cron          *string   `bun:"cron"`
	Timezone      *string   `bun:"timezone"`
	Duration      *int      `bun:"duration"`
	CreatedAt     time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt     time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

func toDomainModelFromSQL(sm *sqlModel) *Model {
	// Parse JSON strings back to arrays
	var weekdays []int
	var daysOfMonth []int

	if sm.Weekdays != "" {
		json.Unmarshal([]byte(sm.Weekdays), &weekdays)
	}
	if sm.DaysOfMonth != "" {
		json.Unmarshal([]byte(sm.DaysOfMonth), &daysOfMonth)
	}

	return &Model{
		ID:            sm.ID,
		Title:         sm.Title,
		Description:   sm.Description,
		Active:        sm.Active,
		Strategy:      sm.Strategy,
		StartDateTime: sm.StartDateTime,
		EndDateTime:   sm.EndDateTime,
		StartTime:     sm.StartTime,
		EndTime:       sm.EndTime,
		Weekdays:      weekdays,
		DaysOfMonth:   daysOfMonth,
		IntervalDay:   sm.IntervalDay,
		Cron:          sm.Cron,
		Timezone:      sm.Timezone,
		Duration:      sm.Duration,
		CreatedAt:     sm.CreatedAt,
		UpdatedAt:     sm.UpdatedAt,
	}
}

type SQLRepositoryImpl struct {
	db *bun.DB
}

func NewSQLRepository(db *bun.DB) Repository {
	return &SQLRepositoryImpl{db: db}
}

func (r *SQLRepositoryImpl) Create(ctx context.Context, entity *CreateUpdateDto) (*Model, error) {
	// Marshal arrays to JSON strings
	weekdaysJSON, _ := json.Marshal(entity.Weekdays)
	daysOfMonthJSON, _ := json.Marshal(entity.DaysOfMonth)

	sm := &sqlModel{
		ID:            uuid.New().String(),
		Title:         entity.Title,
		Description:   entity.Description,
		Active:        entity.Active,
		Strategy:      entity.Strategy,
		StartDateTime: entity.StartDateTime,
		EndDateTime:   entity.EndDateTime,
		StartTime:     entity.StartTime,
		EndTime:       entity.EndTime,
		Weekdays:      string(weekdaysJSON),
		DaysOfMonth:   string(daysOfMonthJSON),
		IntervalDay:   entity.IntervalDay,
		Cron:          entity.Cron,
		Timezone:      entity.Timezone,
		Duration:      entity.Duration,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	_, err := r.db.NewInsert().Model(sm).Returning("*").Exec(ctx)
	if err != nil {
		return nil, err
	}

	return toDomainModelFromSQL(sm), nil
}

func (r *SQLRepositoryImpl) FindByID(ctx context.Context, id string) (*Model, error) {
	sm := new(sqlModel)
	err := r.db.NewSelect().Model(sm).Where("id = ?", id).Scan(ctx)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModelFromSQL(sm), nil
}

func (r *SQLRepositoryImpl) FindAll(ctx context.Context, page int, limit int, q string, strategy string) ([]*Model, error) {
	query := r.db.NewSelect().Model((*sqlModel)(nil))

	if q != "" {
		query = query.Where("LOWER(title) LIKE ? OR LOWER(description) LIKE ?", "%"+q+"%", "%"+q+"%")
	}

	if strategy != "" {
		query = query.Where("strategy = ?", strategy)
	}

	query = query.Order("created_at DESC").
		Limit(limit).
		Offset((page - 1) * limit)

	var sms []*sqlModel
	err := query.Scan(ctx, &sms)
	if err != nil {
		return nil, err
	}

	var models []*Model
	for _, sm := range sms {
		models = append(models, toDomainModelFromSQL(sm))
	}
	return models, nil
}

func (r *SQLRepositoryImpl) UpdateFull(ctx context.Context, id string, entity *CreateUpdateDto) (*Model, error) {
	// Marshal arrays to JSON strings
	weekdaysJSON, _ := json.Marshal(entity.Weekdays)
	daysOfMonthJSON, _ := json.Marshal(entity.DaysOfMonth)

	sm := &sqlModel{
		ID:            id,
		Title:         entity.Title,
		Description:   entity.Description,
		Active:        entity.Active,
		Strategy:      entity.Strategy,
		StartDateTime: entity.StartDateTime,
		EndDateTime:   entity.EndDateTime,
		StartTime:     entity.StartTime,
		EndTime:       entity.EndTime,
		Weekdays:      string(weekdaysJSON),
		DaysOfMonth:   string(daysOfMonthJSON),
		IntervalDay:   entity.IntervalDay,
		Cron:          entity.Cron,
		Timezone:      entity.Timezone,
		Duration:      entity.Duration,
		UpdatedAt:     time.Now(),
	}

	_, err := r.db.NewUpdate().
		Model(sm).
		Where("id = ?", id).
		OmitZero().
		Exec(ctx)
	if err != nil {
		return nil, err
	}

	return toDomainModelFromSQL(sm), nil
}

func (r *SQLRepositoryImpl) UpdatePartial(ctx context.Context, id string, entity *PartialUpdateDto) (*Model, error) {
	query := r.db.NewUpdate().Model((*sqlModel)(nil)).Where("id = ?", id)

	hasUpdates := false

	if entity.Title != nil {
		query = query.Set("title = ?", *entity.Title)
		hasUpdates = true
	}
	if entity.Description != nil {
		query = query.Set("description = ?", *entity.Description)
		hasUpdates = true
	}
	if entity.Active != nil {
		query = query.Set("active = ?", *entity.Active)
		hasUpdates = true
	}
	if entity.Strategy != nil {
		query = query.Set("strategy = ?", *entity.Strategy)
		hasUpdates = true
	}
	if entity.StartDateTime != nil {
		query = query.Set("start_date_time = ?", *entity.StartDateTime)
		hasUpdates = true
	}
	if entity.EndDateTime != nil {
		query = query.Set("end_date_time = ?", *entity.EndDateTime)
		hasUpdates = true
	}
	if entity.StartTime != nil {
		query = query.Set("start_time = ?", *entity.StartTime)
		hasUpdates = true
	}
	if entity.EndTime != nil {
		query = query.Set("end_time = ?", *entity.EndTime)
		hasUpdates = true
	}
	if entity.Weekdays != nil {
		weekdaysJSON, _ := json.Marshal(entity.Weekdays)
		query = query.Set("weekdays = ?", string(weekdaysJSON))
		hasUpdates = true
	}
	if entity.DaysOfMonth != nil {
		daysOfMonthJSON, _ := json.Marshal(entity.DaysOfMonth)
		query = query.Set("days_of_month = ?", string(daysOfMonthJSON))
		hasUpdates = true
	}
	if entity.IntervalDay != nil {
		query = query.Set("interval_day = ?", *entity.IntervalDay)
		hasUpdates = true
	}
	if entity.Cron != nil {
		query = query.Set("cron = ?", *entity.Cron)
		hasUpdates = true
	}
	if entity.Timezone != nil {
		query = query.Set("timezone = ?", *entity.Timezone)
		hasUpdates = true
	}
	if entity.Duration != nil {
		query = query.Set("duration = ?", *entity.Duration)
		hasUpdates = true
	}

	if !hasUpdates {
		return r.FindByID(ctx, id)
	}

	// Always set updated_at
	query = query.Set("updated_at = ?", time.Now())

	_, err := query.Exec(ctx)
	if err != nil {
		return nil, err
	}

	return r.FindByID(ctx, id)
}

func (r *SQLRepositoryImpl) Delete(ctx context.Context, id string) error {
	_, err := r.db.NewDelete().Model((*sqlModel)(nil)).Where("id = ?", id).Exec(ctx)
	return err
}

func (r *SQLRepositoryImpl) SetActive(ctx context.Context, id string, active bool) (*Model, error) {
	_, err := r.db.NewUpdate().
		Model((*sqlModel)(nil)).
		Set("active = ?", active).
		Set("updated_at = ?", time.Now()).
		Where("id = ?", id).
		Exec(ctx)
	if err != nil {
		return nil, err
	}

	return r.FindByID(ctx, id)
}

// GetMaintenancesByMonitorID returns all active maintenances for a given monitor_id
func (r *SQLRepositoryImpl) GetMaintenancesByMonitorID(ctx context.Context, monitorID string) ([]*Model, error) {
	var sms []*sqlModel

	// Use JOIN to get maintenances that are associated with the monitor and are active
	err := r.db.NewSelect().
		Model(&sms).
		Join("JOIN monitor_maintenances mm ON mm.maintenance_id = m.id").
		Where("mm.monitor_id = ? AND m.active = ?", monitorID, true).
		Order("m.updated_at DESC").
		Scan(ctx)

	if err != nil {
		return nil, err
	}

	var models []*Model
	for _, sm := range sms {
		models = append(models, toDomainModelFromSQL(sm))
	}

	return models, nil
}
