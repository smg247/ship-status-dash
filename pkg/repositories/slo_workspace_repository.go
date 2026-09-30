package repositories

import (
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"ship-status-dash/pkg/types"
)

// SLOWorkspaceRepository persists SLO workspace items and their links.
type SLOWorkspaceRepository interface {
	ListByTeam(team string) ([]types.SLOWorkspaceItem, error)
	UpsertItem(item *types.SLOWorkspaceItem) (*types.SLOWorkspaceItem, error)
	DeleteItem(team, kind, itemKey string) error
	DeleteItems(ids []uint) error
	// PruneTeamItems locks the team's rows and deletes the IDs idsFrom returns, in that transaction.
	PruneTeamItems(team string, idsFrom func(items []types.SLOWorkspaceItem) []uint) error
	AddLink(link *types.SLOWorkspaceLink) (*types.SLOWorkspaceLink, error)
	DeleteLink(team, kind, itemKey string, linkID uint) error
}

type gormSLOWorkspaceRepository struct {
	db *gorm.DB
}

func NewGORMSLOWorkspaceRepository(db *gorm.DB) SLOWorkspaceRepository {
	return &gormSLOWorkspaceRepository{db: db}
}

func (r *gormSLOWorkspaceRepository) ListByTeam(team string) ([]types.SLOWorkspaceItem, error) {
	var items []types.SLOWorkspaceItem
	err := r.db.Preload("Links").Where("team = ?", team).Find(&items).Error
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []types.SLOWorkspaceItem{}
	}
	return items, nil
}

func (r *gormSLOWorkspaceRepository) UpsertItem(item *types.SLOWorkspaceItem) (*types.SLOWorkspaceItem, error) {
	var existing types.SLOWorkspaceItem
	err := r.db.Where("team = ? AND kind = ? AND item_key = ?", item.Team, item.Kind, item.ItemKey).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if err := r.db.Create(item).Error; err != nil {
			return nil, err
		}
		return r.getItem(item.Team, item.Kind, item.ItemKey)
	}
	if err != nil {
		return nil, err
	}
	updates := map[string]interface{}{
		"schema_version": item.SchemaVersion,
		"group_key":      item.GroupKey,
		"occurred_at":    item.OccurredAt,
		"outcome":        item.Outcome,
		"details":        item.Details,
		"notes":          item.Notes,
		"updated_by":     item.UpdatedBy,
	}
	if err := r.db.Model(&existing).Updates(updates).Error; err != nil {
		return nil, err
	}
	return r.getItem(item.Team, item.Kind, item.ItemKey)
}

func (r *gormSLOWorkspaceRepository) getItem(team, kind, itemKey string) (*types.SLOWorkspaceItem, error) {
	var item types.SLOWorkspaceItem
	err := r.db.Preload("Links").Where("team = ? AND kind = ? AND item_key = ?", team, kind, itemKey).First(&item).Error
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *gormSLOWorkspaceRepository) DeleteItem(team, kind, itemKey string) error {
	var item types.SLOWorkspaceItem
	err := r.db.Where("team = ? AND kind = ? AND item_key = ?", team, kind, itemKey).First(&item).Error
	if err != nil {
		return err
	}
	return r.DeleteItems([]uint{item.ID})
}

func (r *gormSLOWorkspaceRepository) DeleteItems(ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		return deleteWorkspaceItems(tx, ids)
	})
}

func (r *gormSLOWorkspaceRepository) PruneTeamItems(team string, idsFrom func(items []types.SLOWorkspaceItem) []uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var items []types.SLOWorkspaceItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("team = ?", team).Find(&items).Error; err != nil {
			return err
		}
		return deleteWorkspaceItems(tx, retainedIDs(items, idsFrom(items)))
	})
}

func retainedIDs(items []types.SLOWorkspaceItem, wanted []uint) []uint {
	present := make(map[uint]bool, len(items))
	for _, item := range items {
		present[item.ID] = true
	}
	var ids []uint
	for _, id := range wanted {
		if present[id] {
			ids = append(ids, id)
		}
	}
	return ids
}

func deleteWorkspaceItems(tx *gorm.DB, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	if err := tx.Unscoped().Where("item_id IN ?", ids).Delete(&types.SLOWorkspaceLink{}).Error; err != nil {
		return err
	}
	return tx.Unscoped().Where("id IN ?", ids).Delete(&types.SLOWorkspaceItem{}).Error
}

func (r *gormSLOWorkspaceRepository) AddLink(link *types.SLOWorkspaceLink) (*types.SLOWorkspaceLink, error) {
	var existing types.SLOWorkspaceLink
	err := r.db.Where("item_id = ? AND url = ? AND link_type = ?", link.ItemID, link.URL, link.LinkType).First(&existing).Error
	if err == nil {
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err := r.db.Create(link).Error; err != nil {
		return nil, err
	}
	return link, nil
}

func (r *gormSLOWorkspaceRepository) DeleteLink(team, kind, itemKey string, linkID uint) error {
	item, err := r.getItem(team, kind, itemKey)
	if err != nil {
		return err
	}
	result := r.db.Where("id = ? AND item_id = ?", linkID, item.ID).Delete(&types.SLOWorkspaceLink{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// MockSLOWorkspaceRepository is an in-memory SLO store for handler tests.
type MockSLOWorkspaceRepository struct {
	Items    []types.SLOWorkspaceItem
	ListErr  error
	WriteErr error
}

func (m *MockSLOWorkspaceRepository) ListByTeam(team string) ([]types.SLOWorkspaceItem, error) {
	if m.ListErr != nil {
		return nil, m.ListErr
	}
	var out []types.SLOWorkspaceItem
	for _, item := range m.Items {
		if item.Team == team {
			out = append(out, item)
		}
	}
	if out == nil {
		out = []types.SLOWorkspaceItem{}
	}
	return out, nil
}

func (m *MockSLOWorkspaceRepository) UpsertItem(item *types.SLOWorkspaceItem) (*types.SLOWorkspaceItem, error) {
	if m.WriteErr != nil {
		return nil, m.WriteErr
	}
	for i := range m.Items {
		if m.Items[i].Team == item.Team && m.Items[i].Kind == item.Kind && m.Items[i].ItemKey == item.ItemKey {
			item.ID = m.Items[i].ID
			m.Items[i] = *item
			return &m.Items[i], nil
		}
	}
	item.ID = uint(len(m.Items) + 1)
	m.Items = append(m.Items, *item)
	stored := m.Items[len(m.Items)-1]
	return &stored, nil
}

func (m *MockSLOWorkspaceRepository) DeleteItem(team, kind, itemKey string) error {
	if m.WriteErr != nil {
		return m.WriteErr
	}
	var kept []types.SLOWorkspaceItem
	for _, item := range m.Items {
		if item.Team == team && item.Kind == kind && item.ItemKey == itemKey {
			continue
		}
		kept = append(kept, item)
	}
	m.Items = kept
	return nil
}

func (m *MockSLOWorkspaceRepository) DeleteItems(ids []uint) error {
	if m.WriteErr != nil {
		return m.WriteErr
	}
	drop := map[uint]bool{}
	for _, id := range ids {
		drop[id] = true
	}
	var kept []types.SLOWorkspaceItem
	for _, item := range m.Items {
		if drop[item.ID] {
			continue
		}
		kept = append(kept, item)
	}
	m.Items = kept
	return nil
}

func (m *MockSLOWorkspaceRepository) PruneTeamItems(team string, idsFrom func([]types.SLOWorkspaceItem) []uint) error {
	items, err := m.ListByTeam(team)
	if err != nil {
		return err
	}
	return m.DeleteItems(retainedIDs(items, idsFrom(items)))
}

func (m *MockSLOWorkspaceRepository) AddLink(link *types.SLOWorkspaceLink) (*types.SLOWorkspaceLink, error) {
	if m.WriteErr != nil {
		return nil, m.WriteErr
	}
	link.ID = 1
	return link, nil
}

func (m *MockSLOWorkspaceRepository) DeleteLink(team, kind, itemKey string, linkID uint) error {
	return m.WriteErr
}
