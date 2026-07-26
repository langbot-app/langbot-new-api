package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type legacyModelWithoutUUID struct {
	Id          int    `json:"id"`
	ModelName   string `json:"model_name" gorm:"size:128;not null;uniqueIndex:uk_model_name_delete_at,priority:1"`
	Description string `json:"description,omitempty" gorm:"type:text"`
	Status      int    `json:"status" gorm:"default:1"`
}

func (legacyModelWithoutUUID) TableName() string {
	return "models"
}

func setupModelMetaUUIDTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	LOG_DB = db

	return db
}

func TestEnsureModelUUIDsBackfillsLegacyRows(t *testing.T) {
	db := setupModelMetaUUIDTestDB(t)

	require.NoError(t, db.AutoMigrate(&legacyModelWithoutUUID{}))
	require.NoError(t, db.Create(&legacyModelWithoutUUID{ModelName: "legacy-a", Status: 1}).Error)
	require.NoError(t, db.Create(&legacyModelWithoutUUID{ModelName: "legacy-b", Status: 1}).Error)
	require.NoError(t, db.AutoMigrate(&Model{}))

	require.NoError(t, ensureModelUUIDs())

	var models []Model
	require.NoError(t, db.Order("model_name").Find(&models).Error)
	require.Len(t, models, 2)
	require.NotEmpty(t, models[0].UUID)
	require.NotEmpty(t, models[1].UUID)
	require.NotEqual(t, models[0].UUID, models[1].UUID)
	require.True(t, db.Migrator().HasIndex(&Model{}, "idx_models_uuid_unique"))
}
