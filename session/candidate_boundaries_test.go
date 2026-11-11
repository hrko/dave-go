package session

import (
	"bytes"
	"github.com/thomas-vilte/mls-go/framing"
	"github.com/thomas-vilte/mls-go/group"
	"strings"
	"testing"
)

func TestCandidateBoundKeepsLatest(t *testing.T) {
	s, key := setupActiveSoleMemberSessionWithKey(t)
	s.AddUser("222")
	s.OnDaveMLSProposals(buildProposalBatch(addProposalBytesForUser(t, s, key, "222")))
	oldest := s.commitCandidates[0]
	for range maxCommitCandidates + 1 {
		s.OnDaveMLSProposals(buildRevokeBatch(nil))
	}
	if len(s.commitCandidates) != maxCommitCandidates || len(oldest.CommitBytes()) != 0 {
		t.Fatal("candidate bound or disposal failed")
	}
	confirmLatestCandidate(t, s)
}

func TestCommitCoverageRejectsMissingOrRepeatedRefs(t *testing.T) {
	s, key := setupActiveSoleMemberSessionWithKey(t)
	s.AddUser("111")
	s.OnDaveMLSProposals(buildProposalBatch(addProposalBytesForUser(t, s, key, "111")))
	confirmLatestCandidate(t, s)
	s.OnDaveExecuteTransition(42)
	s.AddUser("222")
	s.AddUser("333")
	p1 := addProposalBytesForUser(t, s, key, "222")
	p2 := addProposalBytesForUser(t, s, key, "333")
	s.OnDaveMLSProposals(buildProposalBatch(append(p1, p2...)))
	if err := s.validateCommitCoverageLocked(s.pendingCommitBytes); err != nil {
		t.Fatalf("valid coverage rejected: %v", err)
	}
	// Structural validation only; signatures are intentionally not re-created.
	for _, mode := range []string{"empty", "partial", "repeat"} {
		wire := mutateMLSMessage(t, s.pendingCommitBytes, func(msg *framing.MLSMessage) {
			pub, _ := msg.AsPublic()
			data, _ := pub.Content.CommitData()
			c, err := group.UnmarshalCommit(data)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "empty":
				c.Proposals = nil
			case "partial":
				c.Proposals = c.Proposals[:1]
			case "repeat":
				c.Proposals[1] = c.Proposals[0]
			}
			pub.Content.Body = framing.CommitBody{Data: c.Marshal()}
		})
		err := s.validateCommitCoverageLocked(wire)
		expected := "commit must cover nonempty cached proposal set"
		if mode == "repeat" {
			expected = "unexpected or repeated commit proposal"
		}
		if err == nil || !strings.Contains(err.Error(), expected) {
			t.Fatalf("%s: expected coverage rejection %q, got %v", mode, expected, err)
		}
	}
}

func TestExternalSenderEstablishedUnchanged(t *testing.T) {
	s, _ := setupActiveSoleMemberSessionWithKey(t)
	before := append([]byte(nil), s.externalSenderPackage...)
	s.OnDaveMLSExternalSenderPackage(buildExternalSenderPackage(t))
	if !bytes.Equal(before, s.externalSenderPackage) {
		t.Fatal("established sender changed")
	}
}

func TestExternalSenderPendingRebuild(t *testing.T) {
	s := New("123456789", testCallbacks{})
	closeAtCleanup(t, s)
	s.SetChannelID(42)
	s.OnSelectProtocolAck(1)
	s.OnDaveMLSExternalSenderPackage(buildExternalSenderPackage(t))
	before := currentGroupBytes(t, s)
	next := buildExternalSenderPackage(t)
	s.OnDaveMLSExternalSenderPackage(next)
	if !bytes.Equal(next, s.externalSenderPackage) || bytes.Equal(before, currentGroupBytes(t, s)) {
		t.Fatal("pending group not rebuilt")
	}
}
