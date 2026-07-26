package model

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupUserQuotaTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	previousDB, previousLogDB := DB, LOG_DB
	previousRedisEnabled := common.RedisEnabled
	previousBatchUpdateEnabled := common.BatchUpdateEnabled
	previousMainDatabaseType, previousLogDatabaseType := common.MainDatabaseType(), common.LogDatabaseType()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	DB = db
	LOG_DB = db
	require.NoError(t, db.AutoMigrate(&User{}))

	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.RedisEnabled = previousRedisEnabled
		common.BatchUpdateEnabled = previousBatchUpdateEnabled
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func useUserQuotaMiniRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	server := miniredis.RunT(t)
	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})
	return server
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

func TestQuotaAndGetRejectsMissingUser(t *testing.T) {
	setupUserQuotaTestDB(t)

	_, err := IncreaseUserQuotaAndGet(404, 10)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestOverrideUserQuotaRejectsNegativeAndKeepsQuota(t *testing.T) {
	db := setupUserQuotaTestDB(t)
	user := &User{
		Username: "quota-negative-override-user",
		Password: "password",
		Status:   common.UserStatusEnabled,
		Quota:    100,
	}
	require.NoError(t, db.Create(user).Error)

	oldQuota, newQuota, err := OverrideUserQuotaAndGet(user.Id, -1)
	require.Error(t, err)
	assert.Zero(t, oldQuota)
	assert.Zero(t, newQuota)

	var quota int
	require.NoError(t, db.Model(&User{}).Where("id = ?", user.Id).Select("quota").Scan(&quota).Error)
	assert.Equal(t, 100, quota)
}

func TestQuotaAndGetMutationsDoNotLoseConcurrentDeltas(t *testing.T) {
	db := setupUserQuotaTestDB(t)
	user := &User{
		Username: "quota-concurrent-user",
		Password: "password",
		Status:   common.UserStatusEnabled,
		Quota:    1000,
	}
	require.NoError(t, db.Create(user).Error)

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, err := IncreaseUserQuotaAndGet(user.Id, 100)
		errs <- err
	}()
	go func() {
		defer wg.Done()
		<-start
		_, err := DecreaseUserQuotaAndGet(user.Id, 50)
		errs <- err
	}()
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	var quota int
	require.NoError(t, db.Model(&User{}).Where("id = ?", user.Id).Select("quota").Scan(&quota).Error)
	assert.Equal(t, 1050, quota)
}

func TestQuotaAndGetInvalidatesCachedUserAfterCommit(t *testing.T) {
	db := setupUserQuotaTestDB(t)
	server := useUserQuotaMiniRedis(t)
	user := User{
		Username: "quota-cache-user",
		Password: "password",
		Status:   common.UserStatusEnabled,
		Quota:    500,
		Group:    "default",
	}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, populateUserCache(user))

	newQuota, err := IncreaseUserQuotaAndGet(user.Id, 200)
	require.NoError(t, err)
	require.Equal(t, 700, newQuota)

	assert.False(t, server.Exists(getUserCacheKey(user.Id)))
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
