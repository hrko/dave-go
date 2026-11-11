package session

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestInvalidCommitClearsReadyAndTransition(t *testing.T) {
	s, _ := setupActiveSoleMemberSessionWithKey(t)
	cb := &downgradeCallbacks{}
	s.callbacks = cb
	s.OnDaveMLSPrepareCommitTransition(7, []byte{0})
	if s.State().Ready || s.pendingTransitionID != 0 || s.sendRatchet != nil {
		t.Fatal("invalid commit retained sending readiness")
	}
	s.OnDavePrepareTransition(8, 1)
	if len(cb.readyTransitionIDs()) != 0 {
		t.Fatal("acknowledged encrypted transition after invalidation")
	}
}

func TestDowngradeRetainsOnlyExpiringReceiveEpoch(t *testing.T) {
	s, _ := setupActiveSoleMemberSessionWithKey(t)
	old := s.activeEpoch
	plain := []byte{0xf8, 1, 2, 3}
	wire := make([]byte, 128)
	n, err := s.Encrypt(1, plain, wire)
	if err != nil {
		t.Fatal(err)
	}
	wire = wire[:n]
	expiredWire := make([]byte, 128)
	n, err = s.Encrypt(1, plain, expiredWire)
	if err != nil {
		t.Fatal(err)
	}
	expiredWire = expiredWire[:n]
	s.OnDavePrepareTransition(7, 0)
	s.OnDaveExecuteTransition(7)
	if s.activeEpoch != nil || s.pendingTransitionID != 0 || len(s.retainedEpoch) != 1 {
		t.Fatal("downgrade state not cleared")
	}
	if old.expiresAt.IsZero() || time.Until(old.expiresAt) > 10*time.Second {
		t.Fatal("receive epoch has no bounded expiry")
	}
	out := make([]byte, 128)
	got, err := s.Decrypt(s.userID, wire, out)
	if err != nil || !bytes.Equal(out[:got], plain) {
		t.Fatalf("in-flight old epoch rejected: %v", err)
	}
	old.expiresAt = time.Now().Add(-time.Second)
	if _, err = s.Decrypt(s.userID, expiredWire, out); err == nil {
		t.Fatal("expired epoch decrypted")
	}
	if len(s.retainedEpoch) != 0 {
		t.Fatal("expired epoch retained")
	}
}

func TestCloseDisposesPreparedCandidates(t *testing.T) {
	s, key := setupActiveSoleMemberSessionWithKey(t)
	s.AddUser("222")
	s.OnDaveMLSProposals(buildProposalBatch(addProposalBytesForUser(t, s, key, "222")))
	h := s.commitCandidates[0]
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.WaitShutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if len(s.commitCandidates) != 0 || len(h.CommitBytes()) != 0 {
		t.Fatal("closed session retained candidate")
	}
}
