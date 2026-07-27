package model

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"gorm.io/gorm"
)

const MaxUserQuotaOperationIDLength = 128
const userQuotaOperationAuditRequestIDPrefix = "quotaop_v1_"
const userQuotaOperationAuditRecoveryBatchSize = 100

var ErrUserQuotaOperationInvalid = errors.New("quota operation_id is invalid")
var ErrUserQuotaOperationMismatch = errors.New("quota operation_id was already used with different parameters")

var userQuotaOperationAuditLocks [64]sync.Mutex

type UserQuotaOperation struct {
	OperationID    string `json:"operation_id" gorm:"column:operation_id;primaryKey;type:varchar(128)"`
	UserID         int    `json:"user_id" gorm:"column:user_id;not null"`
	Mode           string `json:"mode" gorm:"column:mode;type:varchar(16);not null"`
	Value          int    `json:"value" gorm:"column:value;not null"`
	ResultingQuota int    `json:"resulting_quota" gorm:"column:resulting_quota;not null"`
	CreatedAt      int64  `json:"created_at" gorm:"column:created_at"`
	UpdatedAt      int64  `json:"updated_at" gorm:"column:updated_at"`
}

func (UserQuotaOperation) TableName() string {
	return "user_quota_operations"
}

type UserQuotaOperationAudit struct {
	OperationID      string `json:"operation_id" gorm:"column:operation_id;primaryKey;type:varchar(128)"`
	OperatorUserID   int    `json:"operator_user_id" gorm:"column:operator_user_id;not null"`
	OperatorUsername string `json:"operator_username" gorm:"column:operator_username;type:varchar(200);not null"`
	OperatorRole     int    `json:"operator_role" gorm:"column:operator_role;not null"`
	AuthMethod       string `json:"auth_method" gorm:"column:auth_method;type:varchar(32);not null"`
	IP               string `json:"ip" gorm:"column:ip;type:varchar(64);not null"`
	TargetUserID     int    `json:"target_user_id" gorm:"column:target_user_id;not null"`
	Mode             string `json:"mode" gorm:"column:mode;type:varchar(16);not null"`
	Value            int    `json:"value" gorm:"column:value;not null"`
	OldQuota         int    `json:"old_quota" gorm:"column:old_quota;not null"`
	ResultingQuota   int    `json:"resulting_quota" gorm:"column:resulting_quota;not null"`
	LogRequestID     string `json:"log_request_id" gorm:"column:log_request_id;type:varchar(191);not null"`
	LoggedAt         int64  `json:"logged_at" gorm:"column:logged_at;not null;default:0"`
	CreatedAt        int64  `json:"created_at" gorm:"column:created_at"`
	UpdatedAt        int64  `json:"updated_at" gorm:"column:updated_at"`
}

func (UserQuotaOperationAudit) TableName() string {
	return "user_quota_operation_audits"
}

type UserQuotaOperationAuditInput struct {
	OperatorUserID   int
	OperatorUsername string
	OperatorRole     int
	AuthMethod       string
	IP               string
}

type UserQuotaOperationResult struct {
	OldQuota       int
	ResultingQuota int
	Replayed       bool
}

func ApplyUserQuotaOperation(userID int, mode string, value int, operationID string) (UserQuotaOperationResult, error) {
	return ApplyUserQuotaOperationWithAudit(userID, mode, value, operationID, UserQuotaOperationAuditInput{})
}

func ApplyUserQuotaOperationWithAudit(userID int, mode string, value int, operationID string, audit UserQuotaOperationAuditInput) (UserQuotaOperationResult, error) {
	if !IsValidUserQuotaOperationID(operationID) {
		return UserQuotaOperationResult{}, ErrUserQuotaOperationInvalid
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		result, err := applyUserQuotaOperationOnce(userID, mode, value, operationID, audit)
		if err == nil {
			return result, nil
		}
		lastErr = err

		existing, loadErr := getUserQuotaOperation(operationID)
		if loadErr == nil {
			if !sameUserQuotaOperation(existing, userID, mode, value) {
				return UserQuotaOperationResult{}, ErrUserQuotaOperationMismatch
			}
			return UserQuotaOperationResult{
				ResultingQuota: existing.ResultingQuota,
				Replayed:       true,
			}, nil
		}
		if !isRetryableUserQuotaOperationConflict(err) {
			return UserQuotaOperationResult{}, err
		}
		time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
	}
	return UserQuotaOperationResult{}, lastErr
}

func applyUserQuotaOperationOnce(userID int, mode string, value int, operationID string, audit UserQuotaOperationAuditInput) (UserQuotaOperationResult, error) {
	var result UserQuotaOperationResult
	if err := flushPendingUserQuotaForAuthoritativeMutation(userID); err != nil {
		return UserQuotaOperationResult{}, err
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		var existing UserQuotaOperation
		err := tx.Where("operation_id = ?", operationID).First(&existing).Error
		if err == nil {
			if !sameUserQuotaOperation(existing, userID, mode, value) {
				return ErrUserQuotaOperationMismatch
			}
			result.ResultingQuota = existing.ResultingQuota
			result.Replayed = true
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		var user User
		if err := lockForUpdate(tx).Select("id", "quota").Where("id = ?", userID).First(&user).Error; err != nil {
			return err
		}

		err = lockForUpdate(tx).Where("operation_id = ?", operationID).First(&existing).Error
		if err == nil {
			if !sameUserQuotaOperation(existing, userID, mode, value) {
				return ErrUserQuotaOperationMismatch
			}
			result.ResultingQuota = existing.ResultingQuota
			result.Replayed = true
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		result.OldQuota = user.Quota

		switch mode {
		case "add":
			if value < 0 {
				return errors.New("quota 不能为负数！")
			}
			result.ResultingQuota = user.Quota + value
		case "subtract":
			if value < 0 {
				return errors.New("quota 不能为负数！")
			}
			if user.Quota < value {
				return fmt.Errorf("insufficient quota to decrease: current quota is %s, attempted to decrease by %s", logger.FormatQuota(user.Quota), logger.FormatQuota(value))
			}
			result.ResultingQuota = user.Quota - value
		case "override":
			if value < 0 {
				return errors.New("quota 不能为负数！")
			}
			result.ResultingQuota = value
		default:
			return fmt.Errorf("invalid quota operation mode: %s", mode)
		}

		if err := tx.Model(&User{}).Where("id = ?", userID).Update("quota", result.ResultingQuota).Error; err != nil {
			return err
		}

		now := time.Now().Unix()
		operation := UserQuotaOperation{
			OperationID:    operationID,
			UserID:         userID,
			Mode:           mode,
			Value:          value,
			ResultingQuota: result.ResultingQuota,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if err := tx.Create(&operation).Error; err != nil {
			return err
		}
		if audit.OperatorUserID <= 0 {
			return nil
		}
		return tx.Create(&UserQuotaOperationAudit{
			OperationID:      operationID,
			OperatorUserID:   audit.OperatorUserID,
			OperatorUsername: audit.OperatorUsername,
			OperatorRole:     audit.OperatorRole,
			AuthMethod:       audit.AuthMethod,
			IP:               audit.IP,
			TargetUserID:     userID,
			Mode:             mode,
			Value:            value,
			OldQuota:         result.OldQuota,
			ResultingQuota:   result.ResultingQuota,
			LogRequestID:     quotaOperationAuditRequestID(operationID),
			CreatedAt:        now,
			UpdatedAt:        now,
		}).Error
	})
	if err != nil {
		return UserQuotaOperationResult{}, err
	}
	if !result.Replayed {
		invalidateUserQuotaCacheAfterMutation(userID)
	}
	return result, nil
}

func quotaOperationAuditRequestID(operationID string) string {
	sum := sha256.Sum256([]byte(operationID))
	return userQuotaOperationAuditRequestIDPrefix + base64.RawURLEncoding.EncodeToString(sum[:])
}

func IsValidUserQuotaOperationID(operationID string) bool {
	if operationID == "" || len(operationID) > MaxUserQuotaOperationIDLength {
		return false
	}
	return strings.TrimSpace(operationID) == operationID
}

func ReplayUserQuotaOperationAudit(operationID string) (bool, error) {
	return replayUserQuotaOperationAudit(context.Background(), operationID)
}

func replayUserQuotaOperationAudit(ctx context.Context, operationID string) (bool, error) {
	if !IsValidUserQuotaOperationID(operationID) {
		return false, ErrUserQuotaOperationInvalid
	}
	lock := userQuotaOperationAuditLock(operationID)
	lock.Lock()
	defer lock.Unlock()
	var handled bool
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var audit UserQuotaOperationAudit
		err := lockForUpdate(tx).Where("operation_id = ?", operationID).First(&audit).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if audit.LoggedAt > 0 {
			handled = true
			return nil
		}
		logDB := LOG_DB.WithContext(ctx)
		if LOG_DB == DB {
			logDB = tx
		}
		var existingCount int64
		if err := logDB.Model(&Log{}).Where("type = ? AND request_id = ?", LogTypeManage, audit.LogRequestID).Count(&existingCount).Error; err != nil {
			return err
		}
		if existingCount == 0 {
			if err := logDB.Create(buildUserQuotaOperationAuditLog(audit)).Error; err != nil {
				return err
			}
		}
		now := time.Now().Unix()
		if err := tx.Model(&UserQuotaOperationAudit{}).
			Where("operation_id = ? AND logged_at = ?", audit.OperationID, 0).
			Updates(map[string]interface{}{"logged_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
		handled = true
		return nil
	})
	return handled, err
}

func userQuotaOperationAuditLock(operationID string) *sync.Mutex {
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(operationID))
	return &userQuotaOperationAuditLocks[int(hasher.Sum32())%len(userQuotaOperationAuditLocks)]
}

func RecoverPendingUserQuotaOperationAudits(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > userQuotaOperationAuditRecoveryBatchSize {
		limit = userQuotaOperationAuditRecoveryBatchSize
	}
	var audits []UserQuotaOperationAudit
	if err := DB.WithContext(ctx).
		Where("logged_at = ?", 0).
		Order("created_at asc, operation_id asc").
		Limit(limit).
		Find(&audits).Error; err != nil {
		return 0, err
	}
	recovered := 0
	for _, audit := range audits {
		if err := ctx.Err(); err != nil {
			return recovered, err
		}
		handled, err := replayUserQuotaOperationAudit(ctx, audit.OperationID)
		if err != nil {
			return recovered, err
		}
		if handled {
			recovered++
		}
	}
	return recovered, nil
}

func StartUserQuotaOperationAuditRecovery(ctx context.Context, interval time.Duration) <-chan struct{} {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		backoff := interval
		for {
			recovered, err := RecoverPendingUserQuotaOperationAudits(ctx, userQuotaOperationAuditRecoveryBatchSize)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				logger.LogWarn(ctx, fmt.Sprintf("user quota operation audit recovery failed: %v", err))
				if !waitUserQuotaOperationAuditRecovery(ctx, backoff) {
					return
				}
				backoff *= 2
				if backoff > 5*time.Minute {
					backoff = 5 * time.Minute
				}
				continue
			}
			backoff = interval
			if recovered >= userQuotaOperationAuditRecoveryBatchSize {
				continue
			}
			if !waitUserQuotaOperationAuditRecovery(ctx, interval) {
				return
			}
		}
	}()
	return done
}

func waitUserQuotaOperationAuditRecovery(ctx context.Context, interval time.Duration) bool {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func buildUserQuotaOperationAuditLog(audit UserQuotaOperationAudit) *Log {
	action, content, params := userQuotaOperationAuditDetails(audit)
	adminInfo := map[string]interface{}{
		"admin_id":       audit.OperatorUserID,
		"admin_username": audit.OperatorUsername,
		"admin_role":     audit.OperatorRole,
		"auth_method":    audit.AuthMethod,
	}
	other := map[string]interface{}{
		"op":         buildOpField(action, params),
		"admin_info": adminInfo,
	}
	return &Log{
		UserId:    audit.OperatorUserID,
		Username:  audit.OperatorUsername,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeManage,
		Content:   content,
		Ip:        audit.IP,
		RequestId: audit.LogRequestID,
		Other:     common.MapToJsonStr(other),
	}
}

func userQuotaOperationAuditDetails(audit UserQuotaOperationAudit) (string, string, map[string]interface{}) {
	switch audit.Mode {
	case "add":
		params := map[string]interface{}{"quota": logger.LogQuota(audit.Value)}
		if audit.TargetUserID > 0 && audit.TargetUserID != audit.OperatorUserID {
			params["target_user_id"] = audit.TargetUserID
		}
		return "user.quota_add", fmt.Sprintf("Increased user quota by %s", params["quota"]), params
	case "subtract":
		params := map[string]interface{}{"quota": logger.LogQuota(audit.Value)}
		if audit.TargetUserID > 0 && audit.TargetUserID != audit.OperatorUserID {
			params["target_user_id"] = audit.TargetUserID
		}
		return "user.quota_subtract", fmt.Sprintf("Decreased user quota by %s", params["quota"]), params
	case "override":
		params := map[string]interface{}{
			"from": logger.LogQuota(audit.OldQuota),
			"to":   logger.LogQuota(audit.Value),
		}
		if audit.TargetUserID > 0 && audit.TargetUserID != audit.OperatorUserID {
			params["target_user_id"] = audit.TargetUserID
		}
		return "user.quota_override", fmt.Sprintf("Overrode user quota from %s to %s", params["from"], params["to"]), params
	default:
		params := map[string]interface{}{"mode": audit.Mode, "value": audit.Value}
		return "user.quota", "Adjusted user quota", params
	}
}

func getUserQuotaOperation(operationID string) (UserQuotaOperation, error) {
	var operation UserQuotaOperation
	err := DB.Where("operation_id = ?", operationID).First(&operation).Error
	return operation, err
}

func sameUserQuotaOperation(operation UserQuotaOperation, userID int, mode string, value int) bool {
	return operation.UserID == userID && operation.Mode == mode && operation.Value == value
}

func isRetryableUserQuotaOperationConflict(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "deadlock") ||
		strings.Contains(message, "database is locked") ||
		strings.Contains(message, "database table is locked") ||
		strings.Contains(message, "sqlite_busy")
}

func migrateUserQuotaOperations() error {
	var createSQL string
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		createSQL = `CREATE TABLE IF NOT EXISTS "user_quota_operations" (
"operation_id" varchar(128) NOT NULL,
"user_id" integer NOT NULL,
"mode" varchar(16) NOT NULL,
"value" integer NOT NULL,
"resulting_quota" integer NOT NULL,
"created_at" bigint,
"updated_at" bigint,
PRIMARY KEY ("operation_id")
)`
	} else {
		createSQL = "CREATE TABLE IF NOT EXISTS `user_quota_operations` (\n" +
			"`operation_id` varchar(128) NOT NULL,\n" +
			"`user_id` integer NOT NULL,\n" +
			"`mode` varchar(16) NOT NULL,\n" +
			"`value` integer NOT NULL,\n" +
			"`resulting_quota` integer NOT NULL,\n" +
			"`created_at` bigint,\n" +
			"`updated_at` bigint,\n" +
			"PRIMARY KEY (`operation_id`)\n" +
			")"
	}
	return DB.Exec(createSQL).Error
}

func migrateUserQuotaOperationAudits() error {
	var createSQL string
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		createSQL = `CREATE TABLE IF NOT EXISTS "user_quota_operation_audits" (
"operation_id" varchar(128) NOT NULL,
"operator_user_id" integer NOT NULL,
"operator_username" varchar(200) NOT NULL,
"operator_role" integer NOT NULL,
"auth_method" varchar(32) NOT NULL,
"ip" varchar(64) NOT NULL,
"target_user_id" integer NOT NULL,
"mode" varchar(16) NOT NULL,
"value" integer NOT NULL,
"old_quota" integer NOT NULL,
"resulting_quota" integer NOT NULL,
"log_request_id" varchar(191) NOT NULL,
"logged_at" bigint NOT NULL DEFAULT 0,
"created_at" bigint,
"updated_at" bigint,
PRIMARY KEY ("operation_id")
)`
	} else {
		createSQL = "CREATE TABLE IF NOT EXISTS `user_quota_operation_audits` (\n" +
			"`operation_id` varchar(128) NOT NULL,\n" +
			"`operator_user_id` integer NOT NULL,\n" +
			"`operator_username` varchar(200) NOT NULL,\n" +
			"`operator_role` integer NOT NULL,\n" +
			"`auth_method` varchar(32) NOT NULL,\n" +
			"`ip` varchar(64) NOT NULL,\n" +
			"`target_user_id` integer NOT NULL,\n" +
			"`mode` varchar(16) NOT NULL,\n" +
			"`value` integer NOT NULL,\n" +
			"`old_quota` integer NOT NULL,\n" +
			"`resulting_quota` integer NOT NULL,\n" +
			"`log_request_id` varchar(191) NOT NULL,\n" +
			"`logged_at` bigint NOT NULL DEFAULT 0,\n" +
			"`created_at` bigint,\n" +
			"`updated_at` bigint,\n" +
			"PRIMARY KEY (`operation_id`)\n" +
			")"
	}
	return DB.Exec(createSQL).Error
}
