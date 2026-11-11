package session

import (
	"testing"
	"unsafe"

	"github.com/disgoorg/godave"
	"github.com/thomas-vilte/dave-go/mediakeys"
)

func newRatchet(t *testing.T, b byte) *mediakeys.KeyRatchet {
	t.Helper()
	seed := make([]byte, mediakeys.BaseSecretLen)
	for i := range seed {
		seed[i] = b
	}
	r, err := mediakeys.NewKeyRatchet(seed)
	if err != nil {
		t.Fatalf("NewKeyRatchet: %v", err)
	}

	return r
}

// setupForActivation builds a session whose active epoch is `oldMembers` (with
// the bot as sender) and whose pending epoch is `newMembers`, ready for
// activatePendingEpochLocked. The send counter is advanced so a reset is
// observable.
func setupForActivation(t *testing.T, oldMembers, newMembers []godave.UserID) *Session {
	t.Helper()
	s := New("bot", testCallbacks{})
	bot := s.userID

	oldSenders := map[godave.UserID]*senderState{}
	for _, m := range oldMembers {
		oldSenders[m] = &senderState{ratchet: newRatchet(t, 0x01)}
	}
	newSenders := map[godave.UserID]*senderState{}
	for _, m := range newMembers {
		newSenders[m] = &senderState{ratchet: newRatchet(t, 0x02)}
	}

	s.mu.Lock()
	s.activeEpoch = &epochState{id: 0, senders: oldSenders}
	s.sendRatchet = oldSenders[bot].ratchet
	s.sendDomain = s.sendRatchet.DomainIdentity()
	s.hasSendDomain = true
	s.pendingEpoch = &epochState{id: 1, groupID: []byte("g"), senders: newSenders}
	s.pendingGroupID = []byte("g")
	s.mu.Unlock()

	// Advance the counter so we can tell whether activation reset it.
	for range 5 {
		s.sendCounter.Next()
	}

	return s
}

// TestActivate_SkipsSendRetentionOnJoin: when the new epoch adds a member (a
// join), the previous send ratchet must NOT be retained — retaining it would
// black out the joiner (who holds no old-epoch keys) for the whole TTL. Instead
// the bot switches atomically and resets the nonce.
func TestActivate_SkipsSendRetentionOnJoin(t *testing.T) {
	s := setupForActivation(t, []godave.UserID{"bot"}, []godave.UserID{"bot", "joiner"})

	s.mu.Lock()
	s.activatePendingEpochLocked()
	retained := s.retainedSendRatchet
	counter := s.sendCounter.Current()
	sendIsNew := s.sendRatchet == s.activeEpoch.senders["bot"].ratchet
	s.mu.Unlock()

	if retained != nil {
		t.Error("send ratchet must NOT be retained when the new epoch adds a member")
	}
	if counter != 0 {
		t.Errorf("send counter must reset to 0 on atomic switch, got %d", counter)
	}
	if !sendIsNew {
		t.Error("send ratchet must switch to the new epoch's ratchet")
	}
}

// Executing a transition never retains the old send key, including removals.
func TestActivate_DropsOldSendRatchetWhenNoJoin(t *testing.T) {
	t.Run("same members", func(t *testing.T) {
		s := setupForActivation(t, []godave.UserID{"bot", "other"}, []godave.UserID{"bot", "other"})

		s.mu.Lock()
		s.activatePendingEpochLocked()
		retained := s.retainedSendRatchet
		windows := s.stats.TransitionWindows
		s.mu.Unlock()

		if retained != nil {
			t.Error("send ratchet should not be retained when membership is unchanged")
		}
		if windows != 0 {
			t.Errorf("TransitionWindows = %d, want 0", windows)
		}
	})

	t.Run("member removed", func(t *testing.T) {
		s := setupForActivation(t, []godave.UserID{"bot", "leaver"}, []godave.UserID{"bot"})

		s.mu.Lock()
		s.activatePendingEpochLocked()
		retained := s.retainedSendRatchet
		s.mu.Unlock()

		if retained != nil {
			t.Error("send ratchet should not be retained on a removal")
		}
	})
}

func TestActivate_SameKeyPreservesCounter(t *testing.T) {
	s := setupForActivation(t, []godave.UserID{"bot"}, []godave.UserID{"bot"})
	defer s.Close()
	s.pendingEpoch.senders["bot"].ratchet = newRatchet(t, 0x01)
	s.activatePendingEpochLocked()
	if n := s.sendCounter.Current(); n != 5 {
		t.Fatalf("same-key activation reset nonce counter: got %d, want 5", n)
	}
}

func TestActivate_NewDomainAfterLongLivedCounter(t *testing.T) {
	s := setupForActivation(t, []godave.UserID{"bot"}, []godave.UserID{"bot"})
	closeAtCleanup(t, s)
	s.sendRatchet.Commit(256)
	s.sendRatchet.Commit(257)
	// Test-only synthetic counter: avoid billions of packets. Guard the pinned
	// representation and never use this accessor concurrently with encryption.
	if unsafe.Sizeof(*s.sendCounter) != unsafe.Sizeof(uint64(0)) {
		t.Fatal("nonce counter representation changed")
	}
	*(*uint64)(unsafe.Pointer(s.sendCounter)) = uint64(257) << 24
	s.protocolVersion = 1
	s.pendingTransitionID = 8
	s.OnDaveExecuteTransition(8)
	if s.sendCounter.Current() != 0 {
		t.Fatal("new key domain inherited old generation")
	}
	if _, err := s.Encrypt(1, []byte{1, 2, 3}, make([]byte, 128)); err != nil {
		t.Fatalf("new domain cannot encrypt: %v", err)
	}
}

func TestActivate_AfterRatchetDiscard(t *testing.T) {
	for _, same := range []bool{false, true} {
		t.Run(map[bool]string{false: "new domain", true: "same domain"}[same], func(t *testing.T) {
			s := setupForActivation(t, []godave.UserID{"bot"}, []godave.UserID{"bot"})
			closeAtCleanup(t, s)
			s.sendRatchet = nil
			if same {
				s.pendingEpoch.senders["bot"].ratchet = newRatchet(t, 0x01)
			}
			s.activatePendingEpochLocked()
			want := uint64(0)
			if same {
				want = 5
			}
			if got := s.sendCounter.Current(); got != want {
				t.Fatalf("counter=%d, want %d", got, want)
			}
		})
	}
}
