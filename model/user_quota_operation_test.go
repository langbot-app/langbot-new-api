package model

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrateUserQuotaOperationsCreatesDurableTable(t *testing.T) {
	require.NoError(t, migrateUserQuotaOperations())
	require.NoError(t, migrateUserQuotaOperationAudits())
	assert.True(t, DB.Migrator().HasTable(&UserQuotaOperation{}))
	assert.True(t, DB.Migrator().HasTable(&UserQuotaOperationAudit{}))
	t.Cleanup(func() {
		DB.Exec("DELETE FROM user_quota_operation_audits")
		DB.Exec("DELETE FROM user_quota_operations")
	})

	operation := UserQuotaOperation{
		OperationID:    "migration-quota-operation",
		UserID:         12,
		Mode:           "add",
		Value:          100,
		ResultingQuota: 300,
		CreatedAt:      1,
		UpdatedAt:      1,
	}
	require.NoError(t, DB.Create(&operation).Error)
	assert.Error(t, DB.Create(&operation).Error)

	require.NoError(t, migrateUserQuotaOperations())
	require.NoError(t, migrateUserQuotaOperationAudits())
	var count int64
	require.NoError(t, DB.Model(&UserQuotaOperation{}).Where("operation_id = ?", operation.OperationID).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestQuotaOperationAuditRequestIDIsServerNamespacedDigest(t *testing.T) {
	operationID := "historical-request-id"
	sum := sha256.Sum256([]byte(operationID))
	expected := "quotaop_v1_" + base64.RawURLEncoding.EncodeToString(sum[:])

	requestID := quotaOperationAuditRequestID(operationID)

	assert.Equal(t, expected, requestID)
	assert.NotEqual(t, operationID, requestID)
	assert.LessOrEqual(t, len(requestID), 64)
	assert.NotEqual(t, quotaOperationAuditRequestID(operationID+"-other"), requestID)
}

func TestReplayUserQuotaOperationAuditDoesNotTreatHistoricalRequestIDAsOperationLog(t *testing.T) {
	db := setupUserQuotaTestDB(t)
	require.NoError(t, migrateUserQuotaOperations())
	require.NoError(t, migrateUserQuotaOperationAudits())
	user := User{
		Username: "quota-audit-historical-request-user",
		Password: "password",
		Status:   common.UserStatusEnabled,
		Quota:    1000,
	}
	require.NoError(t, db.Create(&user).Error)

	operationID := "historical-request-id"
	require.NoError(t, db.Create(&Log{Type: LogTypeManage, RequestId: operationID, Content: "old manage log"}).Error)
	_, err := ApplyUserQuotaOperationWithAudit(user.Id, "add", 250, operationID, UserQuotaOperationAuditInput{
		OperatorUserID:   1,
		OperatorUsername: "root",
		OperatorRole:     common.RoleRootUser,
		AuthMethod:       "session",
		IP:               "192.0.2.1",
	})
	require.NoError(t, err)

	handled, err := ReplayUserQuotaOperationAudit(operationID)
	require.NoError(t, err)
	require.True(t, handled)

	var logs []Log
	require.NoError(t, db.Where("type = ?", LogTypeManage).Order("request_id asc").Find(&logs).Error)
	require.Len(t, logs, 2)
	assert.Equal(t, operationID, logs[0].RequestId)
	assert.Equal(t, quotaOperationAuditRequestID(operationID), logs[1].RequestId)

	handled, err = ReplayUserQuotaOperationAudit(operationID)
	require.NoError(t, err)
	require.True(t, handled)
	require.NoError(t, db.Where("type = ?", LogTypeManage).Find(&logs).Error)
	assert.Len(t, logs, 2)
}

func TestRecoverPendingUserQuotaOperationAuditsIsBoundedAndIdempotent(t *testing.T) {
	db := setupUserQuotaTestDB(t)
	require.NoError(t, migrateUserQuotaOperations())
	require.NoError(t, migrateUserQuotaOperationAudits())
	user := User{
		Username: "quota-audit-recovery-user",
		Password: "password",
		Status:   common.UserStatusEnabled,
		Quota:    1000,
	}
	require.NoError(t, db.Create(&user).Error)
	for i := 0; i < 3; i++ {
		_, err := ApplyUserQuotaOperationWithAudit(user.Id, "add", 10, fmt.Sprintf("quota-audit-recovery-%d", i), UserQuotaOperationAuditInput{
			OperatorUserID:   1,
			OperatorUsername: "root",
			OperatorRole:     common.RoleRootUser,
			AuthMethod:       "session",
			IP:               "192.0.2.1",
		})
		require.NoError(t, err)
	}

	recovered, err := RecoverPendingUserQuotaOperationAudits(context.Background(), 2)
	require.NoError(t, err)
	assert.Equal(t, 2, recovered)

	var loggedCount int64
	require.NoError(t, db.Model(&UserQuotaOperationAudit{}).Where("logged_at > 0").Count(&loggedCount).Error)
	assert.EqualValues(t, 2, loggedCount)

	recovered, err = RecoverPendingUserQuotaOperationAudits(context.Background(), 2)
	require.NoError(t, err)
	assert.Equal(t, 1, recovered)
	recovered, err = RecoverPendingUserQuotaOperationAudits(context.Background(), 2)
	require.NoError(t, err)
	assert.Equal(t, 0, recovered)

	var logCount int64
	require.NoError(t, db.Model(&Log{}).Where("type = ?", LogTypeManage).Count(&logCount).Error)
	assert.EqualValues(t, 3, logCount)
}

func TestStartUserQuotaOperationAuditRecoveryStopsOnCancel(t *testing.T) {
	db := setupUserQuotaTestDB(t)
	require.NoError(t, migrateUserQuotaOperations())
	require.NoError(t, migrateUserQuotaOperationAudits())
	user := User{
		Username: "quota-audit-sweeper-user",
		Password: "password",
		Status:   common.UserStatusEnabled,
		Quota:    1000,
	}
	require.NoError(t, db.Create(&user).Error)
	_, err := ApplyUserQuotaOperationWithAudit(user.Id, "add", 10, "quota-audit-sweeper", UserQuotaOperationAuditInput{
		OperatorUserID:   1,
		OperatorUsername: "root",
		OperatorRole:     common.RoleRootUser,
		AuthMethod:       "session",
		IP:               "192.0.2.1",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := StartUserQuotaOperationAuditRecovery(ctx, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		var count int64
		require.NoError(t, db.Model(&Log{}).Where("type = ?", LogTypeManage).Count(&count).Error)
		return count == 1
	}, time.Second, 10*time.Millisecond)

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("quota audit recovery worker did not stop after cancellation")
	}
}
