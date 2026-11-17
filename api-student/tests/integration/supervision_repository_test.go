//go:build integration

package integration_test

import (
	"context"
	"errors"
	"testing"

	"api-student/app/model"
	"api-student/app/repository"
)

func TestIncrementSupervised_StopsAtQuota(t *testing.T) {
	ctx := context.Background()
	supervisor := seedSupervisor(t, 2)
	repo := repository.NewSupervisorRepository(pool)

	for i := 1; i <= 2; i++ {
		if _, err := repo.IncrementSupervised(ctx, supervisor.ID); err != nil {
			t.Fatalf("slot ke-%d: error tidak diharapkan: %v", i, err)
		}
	}

	_, err := repo.IncrementSupervised(ctx, supervisor.ID)
	if !errors.Is(err, repository.ErrSupervisorFull) {
		t.Fatalf("slot ke-3: error = %v, seharusnya ErrSupervisorFull", err)
	}

	got, err := repo.FindByID(ctx, supervisor.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.SupervisedCount != 2 {
		t.Errorf("supervised_count = %d, seharusnya 2", got.SupervisedCount)
	}
}

func TestAssignSupervision_Duplicate(t *testing.T) {
	ctx := context.Background()
	user := seedUser(t)
	student := seedStudent(t, user.ID)
	supervisor := seedSupervisor(t, 10)
	repo := repository.NewSupervisionRepository(pool)

	if err := repo.Assign(ctx, student.ID, supervisor.ID); err != nil {
		t.Fatalf("penetapan pertama: %v", err)
	}
	err := repo.Assign(ctx, student.ID, supervisor.ID)
	if !errors.Is(err, repository.ErrAlreadySupervised) {
		t.Errorf("penetapan kedua: error = %v, seharusnya ErrAlreadySupervised", err)
	}
}

// Transaction yang gagal harus membatalkan SEMUA langkahnya, termasuk
// catatan di outbox. Inilah jaminan yang menjadi dasar pola outbox Modul 11.
func TestWithTx_RollsBackEveryStep(t *testing.T) {
	ctx := context.Background()
	supervisor := seedSupervisor(t, 10)

	supervisors := repository.NewSupervisorRepository(pool)
	outbox := repository.NewOutboxRepository(pool)
	txm := repository.NewTxManager(pool)

	errStop := errors.New("gagal yang disengaja")
	err := txm.WithTx(ctx, func(tx repository.DBTX) error {
		if _, err := supervisors.WithTx(tx).IncrementSupervised(ctx, supervisor.ID); err != nil {
			return err
		}
		if err := outbox.WithTx(tx).Insert(ctx, model.EventQuotaChanged,
			model.QuotaChangedEvent{
				SupervisorID: supervisor.ID, SupervisorName: supervisor.Name, Quota: supervisor.Quota,
			}); err != nil {
			return err
		}
		return errStop
	})
	if !errors.Is(err, errStop) {
		t.Fatalf("WithTx: error = %v, seharusnya errStop", err)
	}

	got, _ := supervisors.FindByID(ctx, supervisor.ID)
	if got.SupervisedCount != 0 {
		t.Errorf("supervised_count = %d, seharusnya 0 setelah rollback", got.SupervisedCount)
	}
	var events int
	_ = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM outbox WHERE (payload->>'supervisor_id')::INT = $1`,
		supervisor.ID).Scan(&events)
	if events != 0 {
		t.Errorf("baris outbox = %d, seharusnya 0 setelah rollback", events)
	}
}

func TestClaim_OnlyOnce(t *testing.T) {
	ctx := context.Background()
	user := seedUser(t)
	repo := repository.NewNotificationRepository(pool)

	var outboxID int64
	err := pool.QueryRow(ctx,
		`INSERT INTO outbox (event_type, payload) VALUES ($1, jsonb_build_object('supervisor_id', 1)) RETURNING id`,
		model.EventQuotaChanged).Scan(&outboxID)
	if err != nil {
		t.Fatalf("seed outbox: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM outbox WHERE id = $1`, outboxID)
	})

	n := model.Notification{OutboxID: outboxID, UserID: user.ID, Channel: "email", Subject: "s", Body: "b"}
	first, err := repo.Claim(ctx, n)
	if err != nil || !first {
		t.Fatalf("claim pertama = %v, %v; seharusnya true, nil", first, err)
	}
	second, err := repo.Claim(ctx, n)
	if err != nil || second {
		t.Errorf("claim kedua = %v, %v; seharusnya false, nil", second, err)
	}
}
