package session

import (
	"bytes"
	"context"
	"github.com/thomas-vilte/mls-go/group"
	"testing"
)

// Regression: mls-go's complete-message parser could not split this valid batch.
func TestConcatenatedAppendBatch(t *testing.T) {
	s, key := setupActiveSoleMemberSessionWithKey(t)
	defer s.Close()
	cb := &commitCountingCallbacks{}
	s.callbacks = cb
	s.AddUser("222")
	s.AddUser("333")
	first := addProposalBytesForUser(t, s, key, "222")
	second := addProposalBytesForUser(t, s, key, "333")
	s.OnDaveMLSProposals(buildProposalBatch(append(first, second...)))
	if cb.commits != 1 {
		t.Fatalf("commits=%d, want 1", cb.commits)
	}
	confirmLatestCandidate(t, s)
	if s.pendingEpoch == nil || len(s.pendingEpoch.senders) != 3 {
		t.Fatal("both joiners must be included in the committed epoch")
	}
}

func TestAppendBatchMalformedFramingDoesNotStagePrefix(t *testing.T) {
	for _, trailing := range []bool{false, true} {
		s, key := setupActiveSoleMemberSessionWithKey(t)
		s.AddUser("222")
		s.AddUser("333")
		first := addProposalBytesForUser(t, s, key, "222")
		second := addProposalBytesForUser(t, s, key, "333")
		batch := buildProposalBatch(append(first, second[:len(second)-1]...))
		if trailing {
			batch = append(buildProposalBatch(first), 0)
		}
		before, err := s.mlsClient.store.LoadGroupState(context.Background(), group.NewGroupID(s.groupID))
		if err != nil {
			t.Fatal(err)
		}
		accepted, err := s.processProposalBatchLocked(batch)
		if err == nil || accepted {
			t.Fatalf("accepted malformed framing: %v", err)
		}
		after, err := s.mlsClient.store.LoadGroupState(context.Background(), group.NewGroupID(s.groupID))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Fatal("malformed batch changed MLS state")
		}
		s.Close()
	}
}

func TestAppendBatchRejectsBadSignature(t *testing.T) {
	s, key := setupActiveSoleMemberSessionWithKey(t)
	defer s.Close()
	s.AddUser("222")
	wire := addProposalBytesForUser(t, s, key, "222")
	wire[len(wire)-1] ^= 1
	accepted, err := s.processProposalBatchLocked(buildProposalBatch(wire))
	if err == nil || accepted {
		t.Fatalf("invalid signature accepted: %v", err)
	}
}

func TestAppendBatchRetainsOriginalWire(t *testing.T) {
	s, key := setupActiveSoleMemberSessionWithKey(t)
	defer s.Close()
	first := addProposalBytesForUser(t, s, key, "222")
	second := addProposalBytesForUser(t, s, key, "333")
	items, err := parseAppendProposalMessages(append(append([]byte(nil), first...), second...))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || !bytes.Equal(items[0].wire, first) || !bytes.Equal(items[1].wire, second) {
		t.Fatal("parser did not preserve exact message slices")
	}
}
