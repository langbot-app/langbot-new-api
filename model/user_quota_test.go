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

func setupUserQuotaTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	LOG_DB = db
	require.NoError(t, db.AutoMigrate(&User{}))

	return db
}

func TestDecreaseUserQuotaRequiresSufficientQuota(t *testing.T) {
	db := setupUserQuotaTestDB(t)
	user := &User{
		Username: "quota-user",
		Password: "password",
		Status:   common.UserStatusEnabled,
		Quota:    100,
	}
	require.NoError(t, db.Create(user).Error)

	require.NoError(t, DecreaseUserQuota(user.Id, 40, true))

	var quota int
	require.NoError(t, db.Model(&User{}).Where("id = ?", user.Id).Select("quota").Scan(&quota).Error)
	require.Equal(t, 60, quota)

	require.Error(t, DecreaseUserQuota(user.Id, 100, true))
	require.NoError(t, db.Model(&User{}).Where("id = ?", user.Id).Select("quota").Scan(&quota).Error)
	require.Equal(t, 60, quota)
}

func TestUserValidationAllowsLongEmailCredentialsForSpace(t *testing.T) {
	localPart := strings.Repeat("a", 188)
	email := localPart + "@x.test"
	require.Len(t, email, 195)

	user := &User{
		Username:    email,
		Password:    email,
		DisplayName: email,
		Email:       email,
	}
	require.NoError(t, common.Validate.Struct(user))
}
