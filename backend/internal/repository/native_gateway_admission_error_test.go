//go:build unit

package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestGatewayNativeAdmissionErrorUsesOnlyFinitePostgresCodes(t *testing.T) {
	for _, code := range []string{"40001", "40P01", "08006", "23505", "28000", "55P03", "57014"} {
		t.Run(code, func(t *testing.T) {
			failure := fmt.Errorf("wrapped: %w", &pq.Error{Code: pq.ErrorCode(code), Message: "private database detail"})
			want := service.ErrGatewayNativeIdentity
			if code == "40001" || code == "40P01" {
				want = service.ErrGatewayNativeAdmissionStorageConflict
			}
			require.ErrorIs(t, gatewayNativeAdmissionError(failure), want)
		})
	}
	for _, failure := range []error{nil, sql.ErrNoRows, context.Canceled, context.DeadlineExceeded, errors.New("40001 40P01")} {
		require.ErrorIs(t, gatewayNativeAdmissionError(failure), service.ErrGatewayNativeIdentity)
	}
}

func TestGatewayNativeAdmissionAccountLockClassifiesBeforeGenericMapping(t *testing.T) {
	for _, code := range []string{"40001", "40P01", "57014"} {
		t.Run(code, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT id, created_at").WithArgs(int64(7)).WillReturnError(&pq.Error{Code: pq.ErrorCode(code)})
			mock.ExpectRollback()
			repo := &accountRepository{sql: db}
			account, release, err := repo.LockGatewayNativeAccount(context.Background(), 7)
			require.Nil(t, account)
			require.Nil(t, release)
			want := service.ErrGatewayNativeAdmissionStorageConflict
			if code == "57014" {
				want = service.ErrGatewayNativeIdentity
			}
			require.ErrorIs(t, err, want)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestGatewayNativeAdmissionConnectionReadsPreserveOnlyProtectedSQLCauses(t *testing.T) {
	for _, admission := range []bool{false, true} {
		for _, boundary := range []string{"identity", "account_lock", "intent_lock"} {
			t.Run(fmt.Sprintf("%t/%s", admission, boundary), func(t *testing.T) {
				db, mock, err := sqlmock.New()
				require.NoError(t, err)
				t.Cleanup(func() { _ = db.Close() })
				mock.ExpectBegin()
				first := mock.ExpectQuery("SELECT outcome_account_id")
				if boundary == "identity" {
					first.WillReturnError(&pq.Error{Code: "40001"})
				} else {
					first.WillReturnRows(sqlmock.NewRows([]string{"outcome_account_id"}).AddRow(7))
					second := mock.ExpectQuery("SELECT id FROM accounts")
					if boundary == "account_lock" {
						second.WillReturnError(&pq.Error{Code: "40P01"})
					} else {
						second.WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))
						mock.ExpectQuery("SELECT .* FROM gateway_oauth_connect_intents").WillReturnError(&pq.Error{Code: "40P01"})
					}
				}
				mock.ExpectRollback()
				tx, err := db.BeginTx(context.Background(), nil)
				require.NoError(t, err)
				_, err = nativeConnectionPhysicalRead(context.Background(), tx, service.GatewayNativeCredentialScope{}, "operation", false, admission)
				want := service.ErrGatewayNativeIdentity
				if admission {
					want = service.ErrGatewayNativeAdmissionStorageConflict
				}
				require.ErrorIs(t, err, want)
				require.NoError(t, tx.Rollback())
				require.NoError(t, mock.ExpectationsWereMet())
			})
		}
	}
}
