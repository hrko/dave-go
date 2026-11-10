package session

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"github.com/disgoorg/godave"
	"github.com/thomas-vilte/dave-go/mediakeys"
)

// Specification-facing regression tests. These do not assert a cause for a
// particular running Bot's readiness timeout.
func TestAsterinReviewRevokeNestedVector(t *testing.T) {
	s, _ := setupActiveSoleMemberSessionWithKey(t)
	defer s.Close()
	// TLS vector<ProposalRef>, where ProposalRef is itself opaque<V>.
	// One 32-byte reference therefore has a 33-byte outer body.
	wire := writeVLBytes(writeVLBytes(make([]byte, 32)))
	err := s.processRevokeProposalsLocked(wire)
	if err != nil && strings.Contains(err.Error(), "not multiple") {
		t.Fatalf("valid nested reference rejected before MLS lookup: %v", err)
	}
}

func TestAsterinReviewRevokeKeepsOtherProposal(t *testing.T) {
	s, key := setupActiveSoleMemberSessionWithKey(t)
	defer s.Close()
	cb := &commitCountingCallbacks{}
	s.callbacks = cb
	s.AddUser("222")
	proposal := addProposalBytesForUser(t, s, key, "222")
	// Cache one valid proposal, as exists while proposing/reconciling a commit.
	accepted, err := s.processProposalBatchLocked(append([]byte{0}, writeVLBytes(proposal)...))
	if err != nil || !accepted {
		t.Fatalf("fixture: accepted=%v err=%v", accepted, err)
	}
	// An empty revoke leaves that cached proposal intact; a new commit is required.
	s.OnDaveMLSProposals([]byte{1, 0})
	if cb.commits == 0 {
		t.Fatal("cached proposal remains but no commit was sent")
	}
}

func TestAsterinReviewPrepareTransitionAcknowledged(t *testing.T) {
	s, _ := setupActiveSoleMemberSessionWithKey(t)
	defer s.Close()
	cb := &downgradeCallbacks{}
	s.callbacks = cb
	s.OnDavePrepareTransition(7, 1)
	if ids := cb.readyTransitionIDs(); len(ids) != 1 || ids[0] != 7 {
		t.Fatalf("nonzero transition not acknowledged: %v", ids)
	}
}

type reviewFailedKeyPackage struct{ testCallbacks }

func (reviewFailedKeyPackage) SendMLSKeyPackage([]byte) error {
	return errors.New("synthetic send failure")
}

func TestAsterinReviewUnsentKeyPackage(t *testing.T) {
	s := New("123456789", reviewFailedKeyPackage{})
	defer s.Close()
	s.SetChannelID(42)
	s.OnSelectProtocolAck(1)
	s.OnDaveMLSExternalSenderPackage(buildExternalSenderPackage(t))
	if len(s.groupID) != 0 {
		t.Fatal("local group created from unsent key package")
	}
}

func TestAsterinReviewInitialWaitHasNoRecovery(t *testing.T) {
	cb := &kpCapturingCallbacks{}
	s := New("123456789", cb)
	defer s.Close()
	s.SetChannelID(42)
	s.OnSelectProtocolAck(1)
	s.OnDaveMLSExternalSenderPackage(buildExternalSenderPackage(t))
	if s.Ready() {
		t.Fatal("unexpected readiness before welcome/commit/transition")
	}
	if s.Stats().RecoveryAttempts != 0 {
		t.Fatal("unexpected recovery")
	}
	// No watchdog is registered on this initial path: Close completes promptly.
	_ = s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.WaitShutdown(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestAsterinReviewWelcomeContext(t *testing.T) {
	for _, mismatch := range []string{"channel", "member"} {
		t.Run(mismatch, func(t *testing.T) {
			ctx := context.Background()
			cb := &kpCapturingCallbacks{}
			s := New("123456789", cb)
			defer s.Close()
			s.SetChannelID(42)
			s.OnSelectProtocolAck(1)
			ext := buildExternalSenderPackage(t)
			s.OnDaveMLSExternalSenderPackage(ext)
			gid := make([]byte, 8)
			binary.BigEndian.PutUint64(gid, 42)
			if mismatch == "channel" {
				binary.BigEndian.PutUint64(gid, 43)
				s.AddUser("987654321")
			}
			peer := peerWithIdentity(t, "987654321")
			kp, err := peer.FreshKeyPackageBytes(ctx)
			if err != nil {
				t.Fatal(err)
			}
			gid, err = peer.CreateGroupWithExternalSender(ctx, gid, kp, ext)
			if err != nil {
				t.Fatal(err)
			}
			_, welcome, err := peer.InviteMember(ctx, gid, cb.lastKeyPackage())
			if err != nil {
				t.Fatal(err)
			}
			s.OnDaveMLSWelcome(0, welcome)
			if s.Stats().WelcomesJoined != 0 || s.Ready() {
				t.Fatalf("accepted Welcome with mismatched %s context", mismatch)
			}
		})
	}
}

func TestAsterinReviewExecuteMustSwitchSendKey(t *testing.T) {
	s := newTransitionTestSession(t)
	defer s.Close()
	old := s.sendRatchet
	next, err := mediakeys.NewKeyRatchet(bytes.Repeat([]byte{1}, mediakeys.BaseSecretLen))
	if err != nil {
		t.Fatal(err)
	}
	s.protocolVersion = 1
	s.activeEpoch.senders["222"] = &senderState{ratchet: makeTestRatchet(t)}
	s.pendingEpoch = &epochState{id: 2, senders: map[godave.UserID]*senderState{s.userID: {ratchet: next}}}
	s.pendingTransitionID = 7
	s.OnDaveExecuteTransition(7)
	if s.activeEpoch.id != 2 {
		t.Fatal("fixture did not execute transition")
	}
	if selected := s.selectSendRatchetLocked(); selected == old || selected != next {
		t.Fatal("old send key still selected after removal transition executed")
	}
}
