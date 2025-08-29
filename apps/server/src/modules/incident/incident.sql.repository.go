package incident

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type sqlModel struct {
	bun.BaseModel `bun:"table:incidents,alias:i"`

	ID           string    `bun:"id,pk"`
	Title        string    `bun:"title,notnull"`
	Content      string    `bun:"content,notnull"`
	Style        string    `bun:"style,notnull,default:'warning'"`
	Pin          bool      `bun:"pin,notnull,default:true"`
	Active       bool      `bun:"active,notnull,default:true"`
	StatusPageID *string   `bun:"status_page_id"`
	CreatedAt    time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt    time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

func toDomainModelFromSQL(sm *sqlModel) *Model {
	var statusPageID string
	if sm.StatusPageID != nil {
		statusPageID = *sm.StatusPageID
	}

	return &Model{
		ID:           sm.ID,
		Title:        sm.Title,
		Content:      sm.Content,
		Style:        sm.Style,
		Pin:          sm.Pin,
		Active:       sm.Active,
		StatusPageID: &statusPageID,
		CreatedAt:    sm.CreatedAt,
		UpdatedAt:    sm.UpdatedAt,
	}
}

func toSQLModel(m *Model) *sqlModel {
	var statusPageID *string
	if m.StatusPageID != nil && *m.StatusPageID != "" {
		statusPageID = m.StatusPageID
	}

	return &sqlModel{
		ID:           m.ID,
		Title:        m.Title,
		Content:      m.Content,
		Style:        m.Style,
		Pin:          m.Pin,
		Active:       m.Active,
		StatusPageID: statusPageID,
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
	}
}

func toSQLUpdateModel(m *UpdateModel) map[string]interface{} {
	updates := make(map[string]interface{})

	if m.Title != nil {
		updates["title"] = *m.Title
	}
	if m.Content != nil {
		updates["content"] = *m.Content
	}
	if m.Style != nil {
		updates["style"] = *m.Style
	}
	if m.Pin != nil {
		updates["pin"] = *m.Pin
	}
	if m.Active != nil {
		updates["active"] = *m.Active
	}
	if m.StatusPageID != nil {
		if *m.StatusPageID == "" {
			updates["status_page_id"] = nil
		} else {
			updates["status_page_id"] = *m.StatusPageID
		}
	}

	updates["updated_at"] = time.Now()
	return updates
}

type SQLRepositoryImpl struct {
	db *bun.DB
}

func NewSQLRepository(db *bun.DB) Repository {
	return &SQLRepositoryImpl{db: db}
}

func (r *SQLRepositoryImpl) Create(ctx context.Context, incident *Model) (*Model, error) {
	incident.ID = uuid.New().String()
	incident.CreatedAt = time.Now()
	incident.UpdatedAt = time.Now()

	sqlIncident := toSQLModel(incident)
	_, err := r.db.NewInsert().Model(sqlIncident).Exec(ctx)
	if err != nil {
		return nil, err
	}

	return toDomainModelFromSQL(sqlIncident), nil
}

func (r *SQLRepositoryImpl) FindByID(ctx context.Context, id string) (*Model, error) {
	var sqlIncident sqlModel
	err := r.db.NewSelect().Model(&sqlIncident).Where("id = ?", id).Scan(ctx)
	if err != nil {
		return nil, err
	}

	return toDomainModelFromSQL(&sqlIncident), nil
}

func (r *SQLRepositoryImpl) FindAll(ctx context.Context, page int, limit int, q string) ([]*Model, error) {
	var sqlIncidents []sqlModel
	query := r.db.NewSelect().Model(&sqlIncidents).Order("created_at DESC")

	if q != "" {
		searchTerm := "%" + strings.ToLower(q) + "%"
		query = query.Where("LOWER(title) LIKE ? OR LOWER(content) LIKE ?", searchTerm, searchTerm)
	}

	if page > 0 {
		offset := page * limit
		query = query.Offset(offset)
	}

	if limit > 0 {
		query = query.Limit(limit)
	}

	err := query.Scan(ctx)
	if err != nil {
		return nil, err
	}

	incidents := make([]*Model, len(sqlIncidents))
	for i, sqlIncident := range sqlIncidents {
		incidents[i] = toDomainModelFromSQL(&sqlIncident)
	}

	return incidents, nil
}

func (r *SQLRepositoryImpl) FindByStatusPageID(ctx context.Context, statusPageID string) ([]*Model, error) {
	var sqlIncidents []sqlModel
	err := r.db.NewSelect().
		Model(&sqlIncidents).
		Where("status_page_id = ? AND active = true", statusPageID).
		Order("pin DESC, created_at DESC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	incidents := make([]*Model, len(sqlIncidents))
	for i, sqlIncident := range sqlIncidents {
		incidents[i] = toDomainModelFromSQL(&sqlIncident)
	}

	return incidents, nil
}

func (r *SQLRepositoryImpl) Update(ctx context.Context, id string, incident *UpdateModel) (*Model, error) {
	updates := toSQLUpdateModel(incident)
	if len(updates) == 0 {
		return r.FindByID(ctx, id)
	}

	query := r.db.NewUpdate().Model(&sqlModel{}).Where("id = ?", id)

	for column, value := range updates {
		query = query.Set("? = ?", bun.Ident(column), value)
	}

	_, err := query.Exec(ctx)
	if err != nil {
		return nil, err
	}

	return r.FindByID(ctx, id)
}

func (r *SQLRepositoryImpl) Delete(ctx context.Context, id string) error {
	_, err := r.db.NewDelete().Model(&sqlModel{}).Where("id = ?", id).Exec(ctx)
	return err
}
