package directory

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func newMockRepo(t *testing.T) (*Repo, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &Repo{DB: db}, mock
}
func q(s string) string { return regexp.QuoteMeta(s) }

func TestValidateGroupSnapshotRejectsEmptyWhenLiveSnapshotExists(t *testing.T) {
	repo, mock := newMockRepo(t)
	mock.ExpectQuery("SELECT \\(SELECT COUNT").WillReturnRows(sqlmock.NewRows([]string{"groups", "members"}).AddRow(2, 5))
	err := repo.ValidateGroupSnapshotSize(context.Background(), 0, 0)
	if err == nil {
		t.Fatal("expected empty live group snapshot rejection")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateGroupSnapshotUsesTargetSpecificBaseline(t *testing.T) {
	repo, mock := newMockRepo(t)
	mock.ExpectQuery("SELECT \\(SELECT COUNT").WillReturnRows(sqlmock.NewRows([]string{"groups", "members"}).AddRow(0, 0))
	mock.ExpectQuery("target_code IN \\('user_groups','legacy_full'\\)").WillReturnRows(sqlmock.NewRows([]string{"groups", "members"}).AddRow(100, 100))
	if err := repo.ValidateGroupSnapshotSize(context.Background(), 60, 60); err == nil {
		t.Fatal("expected user-group shrink rejection")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateDirectorySnapshotUsesTargetSpecificBaseline(t *testing.T) {
	repo, mock := newMockRepo(t)
	mock.ExpectQuery("target_code IN \\('directory','legacy_full'\\)").WillReturnRows(sqlmock.NewRows([]string{"departments", "users", "memberships"}).AddRow(100, 100, 100))
	if err := repo.ValidateSnapshotSize(context.Background(), 60, 60, 60, 60, 60); err == nil {
		t.Fatal("expected directory shrink rejection")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEnqueueDueTargetRunRevalidatesAndAuditsInOneTransaction(t *testing.T) {
	repo, mock := newMockRepo(t)
	now := time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC)
	next := now.Add(time.Hour)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT enabled,schedule_type,interval_minutes").WithArgs(TargetDirectory).WillReturnRows(sqlmock.NewRows([]string{"enabled", "schedule_type", "interval_minutes", "daily_time", "timezone", "next", "owner", "until", "now"}).AddRow(true, "interval", 60, "02:00", "UTC", now.Add(-time.Minute), "owner", now.Add(time.Minute), now))
	mock.ExpectQuery("SELECT id FROM directory_sync_runs").WithArgs(TargetDirectory, TargetDirectory).WillReturnError(sql.ErrNoRows)
	mock.ExpectExec("INSERT INTO directory_sync_runs").WithArgs(TargetDirectory).WillReturnResult(sqlmock.NewResult(9, 1))
	mock.ExpectExec("UPDATE sync_target_configs SET last_run_at").WithArgs(next, TargetDirectory).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO audit_logs").WithArgs("9", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	created, err := repo.EnqueueDueTargetRun(context.Background(), TargetDirectory, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected scheduled job")
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateTargetRunAuditFailureRollsBack(t *testing.T) {
	repo, mock := newMockRepo(t)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id FROM directory_sync_runs").WithArgs(TargetDirectory, TargetDirectory).WillReturnError(sql.ErrNoRows)
	mock.ExpectExec("INSERT INTO directory_sync_runs").WithArgs(TargetDirectory, nil, "manual", "pending", int64(7)).WillReturnResult(sqlmock.NewResult(12, 1))
	mock.ExpectExec("INSERT INTO audit_logs").WillReturnError(errors.New("audit unavailable"))
	mock.ExpectRollback()
	userID := int64(7)
	if _, err := repo.CreateTargetRun(context.Background(), TargetDirectory, "manual", &userID, nil, "pending"); err == nil {
		t.Fatal("expected audit failure")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileBatchFailsStrandedBlockedDependency(t *testing.T) {
	repo, mock := newMockRepo(t)
	id := int64(4)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT status FROM directory_sync_runs").WithArgs(id).WillReturnError(sql.ErrNoRows)
	mock.ExpectExec("dependency_missing").WithArgs(id).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\),SUM").WithArgs(id).WillReturnRows(sqlmock.NewRows([]string{"total", "success", "failed", "running"}).AddRow(1, 0, 1, 0))
	mock.ExpectExec("UPDATE sync_batches SET status=\\?,finished_at").WithArgs("failed", id).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := repo.ReconcileBatch(context.Background(), &id); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSchedulerDoesNotClaimBeforeLease(t *testing.T) {
	repo, mock := newMockRepo(t)
	now := time.Now()
	mock.ExpectExec("UPDATE directory_sync_runs SET status='failed'").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT id FROM sync_batches").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT target_code,enabled").WillReturnRows(sqlmock.NewRows([]string{"target_code", "enabled", "schedule_type", "interval_minutes", "daily_time", "timezone", "next_run_at", "last_run_at", "last_success_at", "target_version", "last_error_code", "last_error_message", "updated_at"}))
	mock.ExpectQuery("SELECT id FROM directory_sync_runs WHERE status='pending'").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(44))
	mock.ExpectQuery("SELECT id,target_code,batch_id").WithArgs(int64(44)).WillReturnRows(sqlmock.NewRows([]string{"id", "target_code", "batch_id", "trigger_type", "status", "departments_count", "users_count", "active_users_count", "memberships_count", "active_memberships_count", "groups_count", "group_members_count", "group_fetch_warnings", "member_mapping_errors", "unknown_users_count", "metrics_json", "warnings_json", "target_version", "started_at", "finished_at", "error_code", "error_message", "created_by", "created_at"}).AddRow(44, TargetDirectory, nil, "manual", "pending", 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, nil, nil, 0, nil, nil, "", "", nil, now))
	mock.ExpectExec("UPDATE sync_target_configs SET lease_owner").WithArgs(sqlmock.AnyArg(), 600, TargetDirectory, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 0))
	sched := NewScheduler(repo, nil, "test", nil)
	if err := sched.tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEnqueueDueTargetRunDoesNotDuplicateActiveJob(t *testing.T) {
	repo, mock := newMockRepo(t)
	now := time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC)
	next := now.Add(time.Hour)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT enabled,schedule_type,interval_minutes").WithArgs(TargetDirectory).WillReturnRows(sqlmock.NewRows([]string{"enabled", "schedule_type", "interval_minutes", "daily_time", "timezone", "next", "owner", "until", "now"}).AddRow(true, "interval", 60, "02:00", "UTC", now.Add(-time.Minute), "owner", now.Add(time.Minute), now))
	mock.ExpectQuery("SELECT id FROM directory_sync_runs").WithArgs(TargetDirectory, TargetDirectory).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(3))
	mock.ExpectExec("UPDATE sync_target_configs SET last_run_at").WithArgs(next, TargetDirectory).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	created, err := repo.EnqueueDueTargetRun(context.Background(), TargetDirectory, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("active target must not enqueue duplicate scheduled job")
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMigration0038DownPreservesDirectoryAndRemovesIncompatibleJobs(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "0038_enterprise_sync_targets.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sqlText := string(raw)
	for _, want := range []string{"UPDATE directory_sync_configs", "legacy.directory_version=target.target_version", "DELETE FROM directory_sync_runs", "target_code <> 'legacy_full'"} {
		if !strings.Contains(sqlText, want) {
			t.Fatalf("down migration missing %q", want)
		}
	}
}

func TestUpdateTargetConfigAuditFailureRollsBack(t *testing.T) {
	repo, mock := newMockRepo(t)
	now := time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC)
	next := now.Add(time.Hour)
	cfg := TargetConfig{TargetCode: TargetDirectory, Enabled: true, ScheduleType: "interval", IntervalMinutes: 60, DailyTime: "02:00", Timezone: "UTC"}
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT target_code,enabled,schedule_type").WithArgs(TargetDirectory).WillReturnRows(sqlmock.NewRows([]string{"target_code", "enabled", "schedule_type", "interval_minutes", "daily_time", "timezone", "next", "last", "success", "version", "error_code", "error_message", "updated_at"}).AddRow(TargetDirectory, false, "interval", 360, "02:00", "UTC", nil, nil, nil, 1, "", "", now))
	mock.ExpectExec("UPDATE sync_target_configs SET enabled").WithArgs(true, "interval", 60, "02:00", "UTC", next, int64(8), TargetDirectory).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO audit_logs").WillReturnError(errors.New("audit unavailable"))
	mock.ExpectRollback()
	if err := repo.UpdateTargetConfig(context.Background(), TargetDirectory, cfg, 8, next); err == nil {
		t.Fatal("expected transactional audit failure")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMigration0039PermanentlyFencesLegacyScheduler(t *testing.T) {
	up, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "0039_fence_legacy_directory_scheduler.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	upSQL := string(up)
	for _, want := range []string{"enabled=0", "lease_owner='enterprise-sync-v2-fence'", "lease_until='9999-12-31 23:59:59.999'", "ErrLeaseLost"} {
		if !strings.Contains(upSQL, want) {
			t.Fatalf("0039 up missing %q", want)
		}
	}
	down, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "0039_fence_legacy_directory_scheduler.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	downSQL := string(down)
	for _, want := range []string{"lease_owner=NULL", "lease_until=NULL", "lease_owner='enterprise-sync-v2-fence'"} {
		if !strings.Contains(downSQL, want) {
			t.Fatalf("0039 down missing %q", want)
		}
	}
}

func TestLegacyRenewLeaseLosesOwnershipAfterFence(t *testing.T) {
	repo, mock := newMockRepo(t)
	mock.ExpectExec("UPDATE directory_sync_configs SET lease_until").WithArgs(60, "old-scheduler").WillReturnResult(sqlmock.NewResult(0, 0))
	err := repo.RenewLease(context.Background(), "old-scheduler", time.Minute)
	if !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("RenewLease error=%v want ErrLeaseLost", err)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
