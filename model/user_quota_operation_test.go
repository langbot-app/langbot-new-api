package model

import (
	"testing"

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
