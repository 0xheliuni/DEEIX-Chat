package dberror

import (
	"errors"
	"testing"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"gorm.io/gorm"
)

type sqlStateTestError struct {
	state string
}

func (e sqlStateTestError) Error() string {
	return "database error"
}

func (e sqlStateTestError) SQLState() string {
	return e.state
}

func TestIsRecordNotFound(t *testing.T) {
	t.Parallel()

	if IsRecordNotFound(nil) {
		t.Fatal("nil error should not be record not found")
	}
	if !IsRecordNotFound(gorm.ErrRecordNotFound) {
		t.Fatal("gorm record-not-found should be recognized")
	}
}

func TestIsUniqueConstraint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "gorm duplicate key", err: gorm.ErrDuplicatedKey, want: true},
		{name: "postgres sql state", err: sqlStateTestError{state: "23505"}, want: true},
		{name: "wrapped postgres sql state", err: errors.Join(errors.New("insert user"), sqlStateTestError{state: "23505"}), want: true},
		{name: "postgres duplicate message", err: errors.New(`ERROR: duplicate key value violates unique constraint "users_email_key"`), want: true},
		{name: "sqlite unique message", err: errors.New("UNIQUE constraint failed: users.email"), want: true},
		{name: "sqlite check constraint", err: errors.New("CHECK constraint failed: users_balance_check"), want: false},
		{name: "other error", err: errors.New("connection refused"), want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := IsUniqueConstraint(tt.err); got != tt.want {
				t.Fatalf("IsUniqueConstraint() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTranslate(t *testing.T) {
	t.Parallel()

	other := errors.New("connection refused")
	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "nil", err: nil, want: nil},
		{name: "record not found", err: gorm.ErrRecordNotFound, want: repository.ErrNotFound},
		{name: "duplicate", err: gorm.ErrDuplicatedKey, want: repository.ErrDuplicate},
		{name: "other", err: other, want: other},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := Translate(test.err); got != test.want {
				t.Fatalf("Translate(%v) = %v, want %v", test.err, got, test.want)
			}
		})
	}
}
