package session

import (
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
	s.OnDavePrepareTransition(7, 0)
	s.OnDaveExecuteTransition(7)
	if s.activeEpoch != nil || s.pendingTransitionID != 0 || len(s.retainedEpoch) != 1 {
		t.Fatal("downgrade state not cleared")
	}
	if old.expiresAt.IsZero() || time.Until(old.expiresAt) > 10*time.Second {
		t.Fatal("receive epoch has no bounded expiry")
	}
	old.expiresAt = time.Now().Add(-time.Second)
	s.pruneRetainedEpochsLocked()
	if len(s.retainedEpoch) != 0 {
		t.Fatal("expired epoch retained")
	}
}
