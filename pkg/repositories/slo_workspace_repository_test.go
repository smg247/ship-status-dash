package repositories

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"ship-status-dash/pkg/types"
)

func setupSLODB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.SLOWorkspaceItem{}, &types.SLOWorkspaceLink{}))
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	return db
}

func sloRow(team, kind, itemKey, notes string) *types.SLOWorkspaceItem {
	return &types.SLOWorkspaceItem{
		Team:          team,
		Kind:          kind,
		SchemaVersion: 1,
		ItemKey:       itemKey,
		GroupKey:      "nightly",
		OccurredAt:    time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC),
		Outcome:       "Rejected",
		Details:       []byte(`{"payload_url":"https://example.com"}`),
		Notes:         notes,
		UpdatedBy:     "tester",
	}
}

func TestSLOWorkspaceRepositoryIsolation(t *testing.T) {
	db := setupSLODB(t)
	repo := NewGORMSLOWorkspaceRepository(db)
	kind := "payload_streams"

	trt, err := repo.UpsertItem(sloRow("TRT", kind, "same", "trt"))
	require.NoError(t, err)
	other, err := repo.UpsertItem(sloRow("Widget", kind, "same", "widget"))
	require.NoError(t, err)
	otherKind, err := repo.UpsertItem(sloRow("TRT", "other_kind", "same", "other-kind"))
	require.NoError(t, err)

	updated := sloRow("TRT", kind, "same", "rewritten")
	updated.Outcome = "Accepted"
	stored, err := repo.UpsertItem(updated)
	require.NoError(t, err)
	assert.Equal(t, trt.ID, stored.ID)
	assert.Equal(t, "Accepted", stored.Outcome)
	assert.Equal(t, "rewritten", stored.Notes)

	trtRows, err := repo.ListByTeam("TRT")
	require.NoError(t, err)
	require.Len(t, trtRows, 2)
	widgetRows, err := repo.ListByTeam("Widget")
	require.NoError(t, err)
	require.Len(t, widgetRows, 1)
	assert.Equal(t, "widget", widgetRows[0].Notes)
	assert.Equal(t, other.ID, widgetRows[0].ID)

	require.NoError(t, repo.DeleteItem("TRT", kind, "same"))
	trtRows, err = repo.ListByTeam("TRT")
	require.NoError(t, err)
	require.Len(t, trtRows, 1)
	assert.Equal(t, otherKind.ID, trtRows[0].ID)
	widgetRows, err = repo.ListByTeam("Widget")
	require.NoError(t, err)
	require.Len(t, widgetRows, 1)
}

func TestSLOWorkspaceRepositoryClearsNotes(t *testing.T) {
	db := setupSLODB(t)
	repo := NewGORMSLOWorkspaceRepository(db)
	created, err := repo.UpsertItem(sloRow("TRT", "payload_streams", "k", "keep"))
	require.NoError(t, err)

	cleared := sloRow("TRT", "payload_streams", "k", "")
	cleared.Outcome = "Accepted"
	stored, err := repo.UpsertItem(cleared)
	require.NoError(t, err)
	assert.Equal(t, created.ID, stored.ID)
	assert.Empty(t, stored.Notes)
	assert.Equal(t, "Accepted", stored.Outcome)
}

func TestSLOWorkspaceRepositoryLinkParentAndCleanup(t *testing.T) {
	db := setupSLODB(t)
	repo := NewGORMSLOWorkspaceRepository(db)
	parent, err := repo.UpsertItem(sloRow("TRT", "payload_streams", "parent", "notes"))
	require.NoError(t, err)
	other, err := repo.UpsertItem(sloRow("TRT", "payload_streams", "other", "notes"))
	require.NoError(t, err)
	link, err := repo.AddLink(&types.SLOWorkspaceLink{ItemID: parent.ID, URL: "https://example.com/j", LinkType: "jira"})
	require.NoError(t, err)

	err = repo.DeleteLink("TRT", "payload_streams", other.ItemKey, link.ID)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	var still types.SLOWorkspaceLink
	require.NoError(t, db.First(&still, link.ID).Error)
	assert.Equal(t, parent.ID, still.ItemID)

	require.NoError(t, repo.DeleteItem("TRT", "payload_streams", parent.ItemKey))
	var links []types.SLOWorkspaceLink
	require.NoError(t, db.Unscoped().Find(&links).Error)
	assert.Empty(t, links)
	var items []types.SLOWorkspaceItem
	require.NoError(t, db.Unscoped().Where("item_key = ?", parent.ItemKey).Find(&items).Error)
	assert.Empty(t, items)

	again, err := repo.UpsertItem(sloRow("TRT", "payload_streams", parent.ItemKey, "back"))
	require.NoError(t, err)
	assert.Equal(t, parent.ItemKey, again.ItemKey)
	listed, err := repo.ListByTeam("TRT")
	require.NoError(t, err)
	require.Len(t, listed, 2)
}

func TestSLOWorkspaceRepositoryPruneRemovesLinks(t *testing.T) {
	db := setupSLODB(t)
	repo := NewGORMSLOWorkspaceRepository(db)
	doomed, err := repo.UpsertItem(sloRow("TRT", "payload_streams", "doomed", "notes"))
	require.NoError(t, err)
	kept, err := repo.UpsertItem(sloRow("Widget", "payload_streams", "kept", "notes"))
	require.NoError(t, err)
	_, err = repo.AddLink(&types.SLOWorkspaceLink{ItemID: doomed.ID, URL: "https://example.com/d", LinkType: "other"})
	require.NoError(t, err)
	keptLink, err := repo.AddLink(&types.SLOWorkspaceLink{ItemID: kept.ID, URL: "https://example.com/k", LinkType: "other"})
	require.NoError(t, err)

	require.NoError(t, repo.PruneTeamItems("TRT", func(items []types.SLOWorkspaceItem) []uint {
		ids := make([]uint, 0, len(items))
		for _, item := range items {
			ids = append(ids, item.ID)
		}
		return ids
	}))

	var links []types.SLOWorkspaceLink
	require.NoError(t, db.Unscoped().Find(&links).Error)
	require.Len(t, links, 1)
	assert.Equal(t, keptLink.ID, links[0].ID)
	listed, err := repo.ListByTeam("TRT")
	require.NoError(t, err)
	assert.Empty(t, listed)
	widget, err := repo.ListByTeam("Widget")
	require.NoError(t, err)
	require.Len(t, widget, 1)
	assert.Equal(t, kept.ItemKey, widget[0].ItemKey)

	recreated, err := repo.UpsertItem(sloRow("TRT", "payload_streams", doomed.ItemKey, "again"))
	require.NoError(t, err)
	assert.Equal(t, doomed.ItemKey, recreated.ItemKey)
}
