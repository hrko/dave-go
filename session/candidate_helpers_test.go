package session

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"github.com/disgoorg/godave"
	"github.com/thomas-vilte/mls-go"
	"github.com/thomas-vilte/mls-go/group"
	"testing"
)

func currentGroupBytes(t *testing.T, s *Session) []byte {
	t.Helper()
	raw, err := s.mlsClient.store.LoadGroupState(context.Background(), group.NewGroupID(s.groupID))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func stageValidPeerAdd(t *testing.T, s *Session, peer *mls.Client, key *ecdsa.PrivateKey, gid []byte) {
	t.Helper()
	epoch, err := peer.Epoch(context.Background(), gid)
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("%d", 800000+epoch)
	s.AddUser(godave.UserID(id))
	wire := addProposalBytesForUser(t, s, key, id)
	if _, err = s.processProposalBatchLocked(buildProposalBatch(wire)); err != nil {
		t.Fatal(err)
	}
	if err = peer.ProcessPublicMessage(context.Background(), gid, wire); err != nil {
		t.Fatal(err)
	}
}

func confirmLatestCandidate(t *testing.T, s *Session) {
	t.Helper()
	if len(s.commitCandidates) == 0 {
		t.Fatal("no candidate to confirm")
	}
	s.OnDaveMLSPrepareCommitTransition(42, s.commitCandidates[len(s.commitCandidates)-1].CommitBytes())
	if s.pendingEpoch == nil {
		t.Fatal("candidate was not accepted")
	}
}
