package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func resetUser(t *testing.T, db *DB) (User, Session) {
	t.Helper()
	ctx := context.Background()
	u, err := db.CreateAccount(ctx, AccountInput{
		Handle: "resetter", Email: "resetter@example.com", Password: "correct-horse-battery", Cost: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := db.CreateSession(ctx, u.ID, "127.0.0.1", "test", SessionTTL)
	if err != nil {
		t.Fatal(err)
	}
	return u, sess
}

func TestPasswordResetChangesPasswordAndDropsSessions(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	u, sess := resetUser(t, db)

	token, err := db.CreatePasswordReset(ctx, u.ID, PasswordResetTTL)
	if err != nil {
		t.Fatal(err)
	}
	if len(token) < 32 {
		t.Fatalf("token too short: %q", token)
	}
	if id, err := db.PasswordResetUser(ctx, token); err != nil || id != u.ID {
		t.Fatalf("validate = %d %v", id, err)
	}
	if err := db.ConsumePasswordReset(ctx, token, "a-brand-new-password", 4); err != nil {
		t.Fatal(err)
	}
	_, hash, err := db.FindForLogin(ctx, "resetter")
	if err != nil || !CheckPassword(hash, "a-brand-new-password") {
		t.Fatalf("new password not active: %v", err)
	}
	if CheckPassword(hash, "correct-horse-battery") {
		t.Fatal("old password still works")
	}
	if _, _, err := db.SessionUser(ctx, sess.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("session survived reset: %v", err)
	}
	if _, err := db.PasswordResetUser(ctx, token); !errors.Is(err, ErrPasswordResetInvalid) {
		t.Fatalf("used token still validates: %v", err)
	}
	if err := db.ConsumePasswordReset(ctx, token, "another-new-password", 4); !errors.Is(err, ErrPasswordResetInvalid) {
		t.Fatalf("used token consumed again: %v", err)
	}
}

func TestPasswordResetBoundToPasswordFingerprint(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	u, _ := resetUser(t, db)
	token, err := db.CreatePasswordReset(ctx, u.ID, PasswordResetTTL)
	if err != nil {
		t.Fatal(err)
	}
	// 链接发出之后密码又改了一次：这条链接作废，哪怕它还在窗口内。
	if err := db.SetPassword(ctx, u.ID, "changed-after-issue", 4); err != nil {
		t.Fatal(err)
	}
	if _, err := db.PasswordResetUser(ctx, token); !errors.Is(err, ErrPasswordResetInvalid) {
		t.Fatalf("old link still valid = %v", err)
	}
	if err := db.ConsumePasswordReset(ctx, token, "hijacked-password", 4); !errors.Is(err, ErrPasswordResetInvalid) {
		t.Fatalf("old link consumed = %v", err)
	}
	_, hash, _ := db.FindForLogin(ctx, "resetter")
	if !CheckPassword(hash, "changed-after-issue") {
		t.Fatal("a stale link changed the password")
	}
}

func TestPasswordResetExpiryAndReissue(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	u, _ := resetUser(t, db)

	short, err := db.CreatePasswordReset(ctx, u.ID, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, err := db.PasswordResetUser(ctx, short); !errors.Is(err, ErrPasswordResetExpired) {
		t.Fatalf("expired token = %v", err)
	}
	if err := db.ConsumePasswordReset(ctx, short, "should-not-work", 4); !errors.Is(err, ErrPasswordResetExpired) {
		t.Fatalf("expired consume = %v", err)
	}

	first, err := db.CreatePasswordReset(ctx, u.ID, PasswordResetTTL)
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.CreatePasswordReset(ctx, u.ID, PasswordResetTTL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.PasswordResetUser(ctx, first); !errors.Is(err, ErrPasswordResetInvalid) {
		t.Fatalf("first link survived a reissue: %v", err)
	}
	if _, err := db.PasswordResetUser(ctx, second); err != nil {
		t.Fatalf("second link = %v", err)
	}
	if _, err := db.CreatePasswordReset(ctx, 999999, PasswordResetTTL); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown user = %v", err)
	}
}
