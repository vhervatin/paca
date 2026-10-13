package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	accountdom "github.com/Paca-AI/api/internal/domain/account"
	apikeydom "github.com/Paca-AI/api/internal/domain/apikey"
)

func TestServiceAccountRepository_Lifecycle(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	repo := NewServiceAccountRepository(db)
	keys := NewAPIKeyRepository(db)
	creator := fx.user(false)
	now := time.Now().UTC()

	sa := &accountdom.Account{ID: uuid.New(), Name: "CI bot", Description: "deploys", CreatedBy: &creator, CreatedAt: now, UpdatedAt: now}
	if err := repo.Create(t.Context(), sa); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindByID(t.Context(), sa.ID)
	if err != nil || got.Kind != accountdom.KindServiceAccount || got.Name != "CI bot" || got.Description != "deploys" ||
		got.CreatedBy == nil || *got.CreatedBy != creator {
		t.Fatalf("find = %+v err=%v", got, err)
	}

	// Users and agents are accounts too but are not service accounts.
	if _, err := repo.FindByID(t.Context(), creator); !errors.Is(err, accountdom.ErrServiceAccountNotFound) {
		t.Fatalf("a user must not resolve as a service account: %v", err)
	}
	list, err := repo.List(t.Context())
	if err != nil || len(list) != 1 || list[0].ID != sa.ID {
		t.Fatalf("list = %+v err=%v", list, err)
	}

	// Reads carry the account's workspace roles (sorted by name); project-scoped
	// attachments are not part of them.
	roleB := fx.role(`{"version":"2026-10-01","statements":[]}`)
	roleA := fx.role(`{"version":"2026-10-01","statements":[]}`)
	pgExec(t, db, `UPDATE roles SET name = 'b-role' WHERE id = $1`, roleB)
	pgExec(t, db, `UPDATE roles SET name = 'a-role' WHERE id = $1`, roleA)
	fx.attach(roleB, sa.ID, nil)
	fx.attach(roleA, sa.ID, nil)
	scoped := fx.project()
	fx.attach(fx.role(`{"version":"2026-10-01","statements":[]}`), sa.ID, &scoped)
	withRoles, err := repo.List(t.Context())
	if err != nil || len(withRoles) != 1 || len(withRoles[0].Roles) != 2 ||
		withRoles[0].Roles[0].Name != "a-role" || withRoles[0].Roles[1].Name != "b-role" {
		t.Fatalf("list roles = %+v err=%v", withRoles, err)
	}
	if one, err := repo.FindByID(t.Context(), sa.ID); err != nil || len(one.Roles) != 2 {
		t.Fatalf("find roles = %+v err=%v", one, err)
	}

	got.Name, got.Description = "Release bot", ""
	got.UpdatedAt = time.Now().UTC()
	if err := repo.Update(t.Context(), got); err != nil {
		t.Fatal(err)
	}
	if again, _ := repo.FindByID(t.Context(), sa.ID); again.Name != "Release bot" || again.Description != "" {
		t.Fatalf("update not saved: %+v", again)
	}

	// Its key authenticates it, with its kind; it holds a role and a membership.
	key := &apikeydom.APIKey{ID: uuid.New(), AccountID: sa.ID, CreatedBy: &creator, Name: "ci", KeyPrefix: "abcd1234", CreatedAt: now}
	if err := keys.Create(t.Context(), key, "hash-sa"); err != nil {
		t.Fatal(err)
	}
	found, err := keys.FindByHash(t.Context(), "hash-sa")
	if err != nil || found.AccountID != sa.ID || found.AccountKind != accountdom.KindServiceAccount {
		t.Fatalf("key lookup = %+v err=%v", found, err)
	}
	project := fx.project()
	fx.member(project, sa.ID, false)
	fx.attach(fx.role(`{"version":"2026-10-01","statements":[]}`), sa.ID, nil)

	if err := repo.Delete(t.Context(), sa.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindByID(t.Context(), sa.ID); !errors.Is(err, accountdom.ErrServiceAccountNotFound) {
		t.Fatalf("deleted account still found: %v", err)
	}
	if _, err := keys.FindByHash(t.Context(), "hash-sa"); !errors.Is(err, apikeydom.ErrNotFound) {
		t.Fatalf("a deleted account's key must stop resolving: %v", err)
	}
	var n int
	if err := db.Get(&n, `SELECT count(*) FROM role_attachments WHERE account_id = $1`, sa.ID); err != nil || n != 0 {
		t.Fatalf("attachments left: %d %v", n, err)
	}
	if err := db.Get(&n, `SELECT count(*) FROM project_members WHERE account_id = $1 AND deleted_at IS NULL`, sa.ID); err != nil || n != 0 {
		t.Fatalf("memberships left: %d %v", n, err)
	}
	if err := repo.Delete(t.Context(), sa.ID); !errors.Is(err, accountdom.ErrServiceAccountNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}
