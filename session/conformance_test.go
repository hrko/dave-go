package session

import (
	"bytes"
	"context"
	"testing"

	"github.com/disgoorg/godave"
	"github.com/thomas-vilte/dave-go/mediakeys"
	"github.com/thomas-vilte/mls-go/group"
)

func TestRevokeUnconfirmedCommit(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(map[bool]string{false: "one remains", true: "all removed"}[all], func(t *testing.T) {
			s, key := setupActiveSoleMemberSessionWithKey(t)
			defer s.Close()
			cb := &commitCountingCallbacks{}
			s.callbacks = cb
			s.AddUser("222")
			s.AddUser("333")
			p1 := addProposalBytesForUser(t, s, key, "222")
			p2 := addProposalBytesForUser(t, s, key, "333")
			// Seed the pre-commit cache separately: mls-go's framing decoder
			// currently cannot split a concatenated append batch. That is a
			// separate upstream dependency defect, not a revoke precondition.
			for _, p := range [][]byte{p1, p2} {
				if _, err := s.processProposalBatchLocked(buildProposalBatch(p)); err != nil {
					t.Fatal(err)
				}
			}
			s.OnDaveMLSProposals(buildRevokeBatch(nil))
			if cb.commits != 1 {
				t.Fatalf("initial commit count: %d", cb.commits)
			}
			snapshot, err := group.UnmarshalGroupState(s.preCommitGroupState)
			if err != nil {
				t.Fatal(err)
			}
			proposals := snapshot.StoredProposals()
			if len(proposals) != 2 {
				t.Fatalf("cached proposals: %d", len(proposals))
			}
			refs := append([]byte(nil), proposals[0].Ref...)
			if all {
				refs = append(refs, proposals[1].Ref...)
			}
			s.OnDaveMLSProposals(buildRevokeBatch(refs))
			if all {
				if cb.commits != 1 || s.pendingEpoch != nil {
					t.Fatal("empty cache generated a commit")
				}
			} else {
				if cb.commits != 2 || s.pendingEpoch == nil {
					t.Fatal("remaining proposal was not recommitted")
				}
				if len(s.pendingEpoch.senders) != 2 {
					t.Fatalf("revoked joiner survived: %d members", len(s.pendingEpoch.senders))
				}
			}
		})
	}
}

func TestExecuteFrameUsesNewKey(t *testing.T) {
	s := setupForActivation(t, []godave.UserID{"bot", "leaver"}, []godave.UserID{"bot"})
	defer s.Close()
	s.protocolVersion = 1
	s.pendingTransitionID = 7
	s.OnDaveExecuteTransition(7)
	plain := []byte{0x10, 0x20, 0x30}
	out := make([]byte, 128)
	n, err := s.Encrypt(1, plain, out)
	if err != nil {
		t.Fatal(err)
	}
	receiver := New("receiver", testCallbacks{})
	defer receiver.Close()
	receiver.activeEpoch = &epochState{senders: map[godave.UserID]*senderState{"bot": {ratchet: newRatchet(t, 2), expander: mediakeys.NewNonceExpander()}}}
	decoded := make([]byte, 128)
	m, err := receiver.Decrypt("bot", out[:n], decoded)
	if err != nil || !bytes.Equal(decoded[:m], plain) {
		t.Fatalf("new epoch failed to decrypt: %v", err)
	}
	old := New("removed", testCallbacks{})
	defer old.Close()
	old.activeEpoch = &epochState{senders: map[godave.UserID]*senderState{"bot": {ratchet: newRatchet(t, 1), expander: mediakeys.NewNonceExpander()}}}
	if _, err = old.Decrypt("bot", out[:n], decoded); err == nil {
		t.Fatal("old epoch decrypted post-execute media")
	}
}

func TestRevokeMalformedDoesNotAlterPendingCommit(t *testing.T) {
	s, key := setupActiveSoleMemberSessionWithKey(t)
	defer s.Close()
	s.AddUser("222")
	s.OnDaveMLSProposals(buildProposalBatch(addProposalBytesForUser(t, s, key, "222")))
	pending := s.pendingEpoch
	commit := append([]byte(nil), s.pendingCommitBytes...)
	s.OnDaveMLSProposals([]byte{1, 33, 32})
	if pending != s.pendingEpoch || !bytes.Equal(commit, s.pendingCommitBytes) {
		t.Fatal("malformed revoke mutated pending commit")
	}
}

func TestHandshakeProgress(t *testing.T) {
	s := New("123456789", reviewFailedKeyPackage{})
	defer s.Close()
	s.SetChannelID(42)
	s.OnSelectProtocolAck(1)
	if stats := s.Stats(); stats.KeyPackageSendFailures != 1 || stats.KeyPackagesSent != 0 {
		t.Fatalf("send counters: %+v", stats)
	}
	if s.State().HasKeyPackage {
		t.Fatal("failed send marked as published")
	}
	s.callbacks = &kpCapturingCallbacks{}
	s.OnSelectProtocolAck(1)
	s.OnDaveMLSExternalSenderPackage(buildExternalSenderPackage(t))
	st := s.State()
	if !st.HasKeyPackage || !st.HasExternalSender || !st.HasGroup || st.HasPendingCommit || st.HasPendingEpoch || st.Ready {
		t.Fatalf("initial wait state: %+v", st)
	}
	if s.Stats().KeyPackagesSent != 1 {
		t.Fatal("successful send not counted")
	}
	s.OnDavePrepareTransition(0, 1)
	if !s.State().Ready {
		t.Fatal("sole member transition did not activate")
	}
	_ = s.Close()
	if err := s.WaitShutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func FuzzRevokeReferences(f *testing.F) {
	f.Add([]byte{0})
	f.Add(writeVLBytes(writeVLBytes(make([]byte, 32))))
	f.Add([]byte{0xff})
	f.Fuzz(func(t *testing.T, p []byte) {
		refs, err := parseRevokeReferences(p)
		if err != nil {
			return
		}
		if len(refs) > maxRevokeRefs {
			t.Fatal("unbounded refs")
		}
		for _, r := range refs {
			if len(r) != proposalRefSize {
				t.Fatal("invalid ref size")
			}
		}
	})
}
