package dberror

import (
	"errors"
	"strings"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"gorm.io/gorm"
)

type sqlStateError interface {
	SQLState() string
}

// IsRecordNotFound 判断底层错误是否表示记录不存在。
func IsRecordNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

// IsUniqueConstraint 判断底层错误是否表示唯一约束冲突。
func IsUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	var stateErr sqlStateError
	if errors.As(err, &stateErr) && stateErr.SQLState() == "23505" {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "unique constraint failed")
}

// Translate 将通用数据库错误映射为仓储契约错误。
func Translate(err error) error {
	if err == nil {
		return nil
	}
	if IsRecordNotFound(err) {
		return repository.ErrNotFound
	}
	if IsUniqueConstraint(err) {
		return repository.ErrDuplicate
	}
	return err
}
