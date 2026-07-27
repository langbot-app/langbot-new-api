package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupManageUserTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedisEnabled := common.RedisEnabled
	previousMainDatabaseType, previousLogDatabaseType := common.MainDatabaseType(), common.LogDatabaseType()
	common.RedisEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_pragma=busy_timeout(5000)", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(4)
	model.DB, model.LOG_DB = db, db
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.UserSession{}, &model.Log{}, &model.CasbinRule{}, &model.AuthzRole{},
	))
	require.NoError(t, db.Exec(`CREATE TABLE user_quota_operations (
		operation_id varchar(128) NOT NULL,
		user_id integer NOT NULL,
		mode varchar(16) NOT NULL,
		value integer NOT NULL,
		resulting_quota integer NOT NULL,
		created_at bigint,
		updated_at bigint,
		PRIMARY KEY (operation_id)
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE user_quota_operation_audits (
		operation_id varchar(128) NOT NULL,
		operator_user_id integer NOT NULL,
		operator_username varchar(200) NOT NULL,
		operator_role integer NOT NULL,
		auth_method varchar(32) NOT NULL,
		ip varchar(64) NOT NULL,
		target_user_id integer NOT NULL,
		mode varchar(16) NOT NULL,
		value integer NOT NULL,
		old_quota integer NOT NULL,
		resulting_quota integer NOT NULL,
		log_request_id varchar(191) NOT NULL,
		logged_at bigint NOT NULL DEFAULT 0,
		created_at bigint,
		updated_at bigint,
		PRIMARY KEY (operation_id)
	)`).Error)

	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled = previousRedisEnabled
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		_ = sqlDB.Close()
	})
	return db
}

func performManageUserRequest(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/manage", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("id", 9999)
	c.Set("role", common.RoleRootUser)
	c.Set("username", "root-operator")
	ManageUser(c)
	return recorder
}

func TestManageUserDisableAdvancesAuthVersionOnceAndRevokesSession(t *testing.T) {
	db := setupManageUserTestDB(t)
	now := time.Now().Unix()
	user := model.User{
		Username: "managed-disable-user", Password: "password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1,
	}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&model.UserSession{
		SID: "managed-disable-session", UserID: user.Id, Version: 1, UserAuthVersion: 1,
		Status: model.UserSessionStatusActive, RefreshHash: "refresh-hash", LoginMethod: "password",
		LastActiveAt: now, ExpiresAt: now + 3600,
	}).Error)

	recorder := performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"disable"}`, user.Id))
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":true`)

	var updated model.User
	require.NoError(t, db.First(&updated, user.Id).Error)
	assert.Equal(t, common.UserStatusDisabled, updated.Status)
	assert.EqualValues(t, 2, updated.AuthVersion)
	var session model.UserSession
	require.NoError(t, db.First(&session, "sid = ?", "managed-disable-session").Error)
	assert.Equal(t, model.UserSessionStatusRevoked, session.Status)
}

func TestManageUserDemoteAdvancesAuthVersionAndRevokesSessionsOnce(t *testing.T) {
	db := setupManageUserTestDB(t)
	previousMaster := common.IsMasterNode
	common.IsMasterNode = false
	t.Cleanup(func() { common.IsMasterNode = previousMaster })
	require.NoError(t, authz.Init(db))

	now := time.Now().Unix()
	user := model.User{
		Username: "managed-demote-user", Password: "password", Role: common.RoleAdminUser,
		Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1,
	}
	require.NoError(t, db.Create(&user).Error)
	for _, sid := range []string{"managed-demote-session-one", "managed-demote-session-two"} {
		require.NoError(t, db.Create(&model.UserSession{
			SID: sid, UserID: user.Id, Version: 1, UserAuthVersion: 1,
			Status: model.UserSessionStatusActive, RefreshHash: "refresh-" + sid, LoginMethod: "password",
			LastActiveAt: now, ExpiresAt: now + 3600,
		}).Error)
	}

	sessionUpdateCount := 0
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:count_demote_session_updates", func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "user_sessions" {
			sessionUpdateCount++
		}
	}))

	recorder := performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"demote"}`, user.Id))
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":true`)

	var updated model.User
	require.NoError(t, db.First(&updated, user.Id).Error)
	assert.Equal(t, common.RoleCommonUser, updated.Role)
	assert.EqualValues(t, 2, updated.AuthVersion)
	var sessions []model.UserSession
	require.NoError(t, db.Where("user_id = ?", user.Id).Order("sid asc").Find(&sessions).Error)
	require.Len(t, sessions, 2)
	for _, session := range sessions {
		assert.Equal(t, model.UserSessionStatusRevoked, session.Status)
		assert.Equal(t, "admin_demote", session.RevokedReason)
	}
	assert.Equal(t, 1, sessionUpdateCount)
}

func TestManageUserDeleteReturnsImmediatelyAndUnknownActionFails(t *testing.T) {
	db := setupManageUserTestDB(t)
	deleted := model.User{
		Username: "managed-delete-user", Password: "password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1, AffCode: "delete-aff",
	}
	require.NoError(t, db.Create(&deleted).Error)

	recorder := performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"delete"}`, deleted.Id))
	assert.Contains(t, recorder.Body.String(), `"success":true`)
	var deletedCount int64
	require.NoError(t, db.Unscoped().Model(&model.User{}).Where("id = ? AND deleted_at IS NOT NULL", deleted.Id).Count(&deletedCount).Error)
	assert.EqualValues(t, 1, deletedCount)

	unchanged := model.User{
		Username: "managed-unknown-user", Password: "password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1, AffCode: "unknown-aff",
	}
	require.NoError(t, db.Create(&unchanged).Error)
	recorder = performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"unknown"}`, unchanged.Id))
	assert.Contains(t, recorder.Body.String(), `"success":false`)
	require.NoError(t, db.First(&unchanged, unchanged.Id).Error)
	assert.EqualValues(t, 1, unchanged.AuthVersion)
	assert.Equal(t, common.UserStatusEnabled, unchanged.Status)
}

func TestManageUserQuotaReturnsAuthoritativeDataObject(t *testing.T) {
	db := setupManageUserTestDB(t)
	user := model.User{
		Username: "managed-quota-user", Password: "password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", Quota: 1000, AuthVersion: 1,
	}
	require.NoError(t, db.Create(&user).Error)

	recorder := performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":"add","value":250,"operation_id":"quota-op-authoritative-data"}`, user.Id))
	assert.Equal(t, http.StatusOK, recorder.Code)

	var response struct {
		Success bool           `json:"success"`
		Data    map[string]int `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, map[string]int{"quota": 1250}, response.Data)

	var updated model.User
	require.NoError(t, db.Select("quota").First(&updated, user.Id).Error)
	assert.Equal(t, 1250, updated.Quota)
}

func TestManageUserQuotaOperationIdReplaysOriginalResult(t *testing.T) {
	db := setupManageUserTestDB(t)
	user := model.User{
		Username: "managed-quota-idempotent-user", Password: "password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", Quota: 1000, AuthVersion: 1,
	}
	require.NoError(t, db.Create(&user).Error)

	body := fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":"add","value":250,"operation_id":"quota-op-replay"}`, user.Id)
	first := performManageUserRequest(t, body)
	second := performManageUserRequest(t, body)

	for _, recorder := range []*httptest.ResponseRecorder{first, second} {
		assert.Equal(t, http.StatusOK, recorder.Code)
		var response struct {
			Success bool           `json:"success"`
			Data    map[string]int `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		require.True(t, response.Success)
		assert.Equal(t, map[string]int{"quota": 1250}, response.Data)
	}

	var updated model.User
	require.NoError(t, db.Select("quota").First(&updated, user.Id).Error)
	assert.Equal(t, 1250, updated.Quota)

	var operation struct {
		UserID         int    `gorm:"column:user_id"`
		Mode           string `gorm:"column:mode"`
		Value          int    `gorm:"column:value"`
		ResultingQuota int    `gorm:"column:resulting_quota"`
	}
	require.NoError(t, db.Table("user_quota_operations").Where("operation_id = ?", "quota-op-replay").First(&operation).Error)
	assert.Equal(t, user.Id, operation.UserID)
	assert.Equal(t, "add", operation.Mode)
	assert.Equal(t, 250, operation.Value)
	assert.Equal(t, 1250, operation.ResultingQuota)

	var count int64
	require.NoError(t, db.Table("user_quota_operations").Where("operation_id = ?", "quota-op-replay").Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestManageUserQuotaOperationIdReplayDoesNotUseCurrentQuotaOrAudit(t *testing.T) {
	db := setupManageUserTestDB(t)
	user := model.User{
		Username: "managed-quota-replay-after-change-user", Password: "password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", Quota: 1000, AuthVersion: 1,
	}
	require.NoError(t, db.Create(&user).Error)

	body := fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":"add","value":250,"operation_id":"quota-op-authoritative"}`, user.Id)
	first := performManageUserRequest(t, body)
	assert.Contains(t, first.Body.String(), `"success":true`)

	unrelated := performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":"add","value":100,"operation_id":"quota-op-unrelated-change"}`, user.Id))
	assert.Contains(t, unrelated.Body.String(), `"success":true`)

	replay := performManageUserRequest(t, body)
	assert.Equal(t, http.StatusOK, replay.Code)
	var response struct {
		Success bool           `json:"success"`
		Data    map[string]int `json:"data"`
	}
	require.NoError(t, common.Unmarshal(replay.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, map[string]int{"quota": 1250}, response.Data)

	var updated model.User
	require.NoError(t, db.Select("quota").First(&updated, user.Id).Error)
	assert.Equal(t, 1350, updated.Quota)

	var auditCount int64
	require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeManage).Count(&auditCount).Error)
	assert.EqualValues(t, 2, auditCount)
}

func TestManageUserQuotaOperationIdConcurrentSameOperationAppliesOnce(t *testing.T) {
	db := setupManageUserTestDB(t)
	user := model.User{
		Username: "managed-quota-concurrent-op-user", Password: "password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", Quota: 1000, AuthVersion: 1,
	}
	require.NoError(t, db.Create(&user).Error)

	body := fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":"add","value":250,"operation_id":"quota-op-concurrent"}`, user.Id)
	start := make(chan struct{})
	recorders := make(chan *httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			<-start
			recorders <- performManageUserRequest(t, body)
		}()
	}
	close(start)
	wg.Wait()
	close(recorders)

	for recorder := range recorders {
		assert.Equal(t, http.StatusOK, recorder.Code)
		var response struct {
			Success bool           `json:"success"`
			Data    map[string]int `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		require.True(t, response.Success, recorder.Body.String())
		assert.Equal(t, map[string]int{"quota": 1250}, response.Data)
	}

	var updated model.User
	require.NoError(t, db.Select("quota").First(&updated, user.Id).Error)
	assert.Equal(t, 1250, updated.Quota)

	var operationCount int64
	require.NoError(t, db.Table("user_quota_operations").Where("operation_id = ?", "quota-op-concurrent").Count(&operationCount).Error)
	assert.EqualValues(t, 1, operationCount)

	var auditCount int64
	require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeManage).Count(&auditCount).Error)
	assert.EqualValues(t, 1, auditCount)
}

func TestManageUserQuotaOperationAuditOutboxReplayAfterQuotaOperationCommitRecordsExactlyOnce(t *testing.T) {
	db := setupManageUserTestDB(t)
	user := model.User{
		Username: "managed-quota-audit-replay-user", Password: "password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", Quota: 1000, AuthVersion: 1,
	}
	require.NoError(t, db.Create(&user).Error)

	_, err := model.ApplyUserQuotaOperationWithAudit(user.Id, "add", 250, "quota-op-audit-replay", model.UserQuotaOperationAuditInput{
		OperatorUserID:   9999,
		OperatorUsername: "root-operator",
		OperatorRole:     common.RoleRootUser,
		AuthMethod:       "session",
		IP:               "192.0.2.10",
	})
	require.NoError(t, err)

	var auditCount int64
	require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeManage).Count(&auditCount).Error)
	require.EqualValues(t, 0, auditCount)

	body := fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":"add","value":250,"operation_id":"quota-op-audit-replay"}`, user.Id)
	firstReplay := performManageUserRequest(t, body)
	secondReplay := performManageUserRequest(t, body)
	assert.Contains(t, firstReplay.Body.String(), `"success":true`)
	assert.Contains(t, secondReplay.Body.String(), `"success":true`)

	require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeManage).Count(&auditCount).Error)
	assert.EqualValues(t, 1, auditCount)

	var auditLog model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeManage).First(&auditLog).Error)
	assert.NotEqual(t, "quota-op-audit-replay", auditLog.RequestId)
	assert.True(t, strings.HasPrefix(auditLog.RequestId, "quotaop_v1_"))
	assert.Contains(t, auditLog.Content, "Increased user quota")
}

func TestManageUserQuotaOperationIdRejectsMismatchedReplay(t *testing.T) {
	db := setupManageUserTestDB(t)
	user := model.User{
		Username: "managed-quota-mismatch-user", Password: "password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", Quota: 1000, AuthVersion: 1,
	}
	require.NoError(t, db.Create(&user).Error)

	first := performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":"add","value":250,"operation_id":"quota-op-mismatch"}`, user.Id))
	assert.Contains(t, first.Body.String(), `"success":true`)

	second := performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":"add","value":300,"operation_id":"quota-op-mismatch"}`, user.Id))
	assert.Equal(t, http.StatusOK, second.Code)
	assert.Contains(t, second.Body.String(), `"success":false`)

	var updated model.User
	require.NoError(t, db.Select("quota").First(&updated, user.Id).Error)
	assert.Equal(t, 1250, updated.Quota)

	var count int64
	require.NoError(t, db.Table("user_quota_operations").Where("operation_id = ?", "quota-op-mismatch").Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestManageUserQuotaOperationIdValidation(t *testing.T) {
	db := setupManageUserTestDB(t)
	user := model.User{
		Username: "managed-quota-operation-id-validation-user", Password: "password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", Quota: 1000, AuthVersion: 1,
	}
	require.NoError(t, db.Create(&user).Error)

	bodies := []string{
		fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":"add","value":250}`, user.Id),
		fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":"add","value":250,"operation_id":""}`, user.Id),
		fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":"add","value":250,"operation_id":"   "}`, user.Id),
		fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":"add","value":250,"operation_id":" quota-op-spaced"}`, user.Id),
		fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":"add","value":250,"operation_id":"%s"}`, user.Id, strings.Repeat("a", model.MaxUserQuotaOperationIDLength+1)),
	}
	for _, body := range bodies {
		recorder := performManageUserRequest(t, body)
		assert.Equal(t, http.StatusOK, recorder.Code)
		assert.Contains(t, recorder.Body.String(), `"success":false`)
	}

	var updated model.User
	require.NoError(t, db.Select("quota").First(&updated, user.Id).Error)
	assert.Equal(t, 1000, updated.Quota)
}

func TestManageUserQuotaRequiresOperationIdForEveryMode(t *testing.T) {
	db := setupManageUserTestDB(t)
	user := model.User{
		Username: "managed-quota-operation-id-required-user", Password: "password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", Quota: 1000, AuthVersion: 1,
	}
	require.NoError(t, db.Create(&user).Error)

	for _, mode := range []string{"add", "subtract", "override"} {
		recorder := performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":"%s","value":100}`, user.Id, mode))
		assert.Equal(t, http.StatusOK, recorder.Code)
		assert.Contains(t, recorder.Body.String(), `"success":false`)
	}

	var updated model.User
	require.NoError(t, db.Select("quota").First(&updated, user.Id).Error)
	assert.Equal(t, 1000, updated.Quota)

	var operationCount int64
	require.NoError(t, db.Model(&model.UserQuotaOperation{}).Count(&operationCount).Error)
	assert.EqualValues(t, 0, operationCount)
}

func TestManageUserQuotaRejectsNegativeOverride(t *testing.T) {
	db := setupManageUserTestDB(t)
	user := model.User{
		Username: "managed-negative-quota-user", Password: "password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", Quota: 1000, AuthVersion: 1,
	}
	require.NoError(t, db.Create(&user).Error)

	recorder := performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":"override","value":-1,"operation_id":"quota-op-negative-override"}`, user.Id))
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":false`)

	var updated model.User
	require.NoError(t, db.Select("quota").First(&updated, user.Id).Error)
	assert.Equal(t, 1000, updated.Quota)
}

func TestManageUserQuotaRejectsMissingUser(t *testing.T) {
	db := setupManageUserTestDB(t)
	user := model.User{
		Username: "managed-omitted-id-user", Password: "password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", Quota: 1000, AuthVersion: 1,
	}
	require.NoError(t, db.Create(&user).Error)

	recorder := performManageUserRequest(t, `{"id":404,"action":"add_quota","mode":"add","value":250,"operation_id":"quota-op-missing-user"}`)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":false`)

	recorder = performManageUserRequest(t, `{"action":"add_quota","mode":"add","value":250,"operation_id":"quota-op-missing-id"}`)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":false`)

	var updated model.User
	require.NoError(t, db.Select("quota").First(&updated, user.Id).Error)
	assert.Equal(t, 1000, updated.Quota)
}
