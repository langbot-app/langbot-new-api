package model

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"gorm.io/gorm"
)

const MaxUserQuotaOperationIDLength = 128

var ErrUserQuotaOperationInvalid = errors.New("quota operation_id is invalid")
var ErrUserQuotaOperationMismatch = errors.New("quota operation_id was already used with different parameters")

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

type UserQuotaOperationResult struct {
	OldQuota       int
	ResultingQuota int
	Replayed       bool
}

func ApplyUserQuotaOperation(userID int, mode string, value int, operationID string) (UserQuotaOperationResult, error) {
	if strings.TrimSpace(operationID) != operationID || operationID == "" || len(operationID) > MaxUserQuotaOperationIDLength {
		return UserQuotaOperationResult{}, ErrUserQuotaOperationInvalid
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		result, err := applyUserQuotaOperationOnce(userID, mode, value, operationID)
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

func applyUserQuotaOperationOnce(userID int, mode string, value int, operationID string) (UserQuotaOperationResult, error) {
	var result UserQuotaOperationResult
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
		return tx.Create(&operation).Error
	})
	if err != nil {
		return UserQuotaOperationResult{}, err
	}
	if !result.Replayed {
		invalidateUserQuotaCacheAfterMutation(userID)
	}
	return result, nil
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
