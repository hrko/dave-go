package session

import (
	"bytes"
	"context"
	"github.com/thomas-vilte/mls-go/ciphersuite"
	"github.com/thomas-vilte/mls-go/framing"
	"github.com/thomas-vilte/mls-go/group"
	"testing"
)

func TestCandidateMultipleBatchesStayAtBaseEpoch(t *testing.T) {
	s, key := setupActiveSoleMemberSessionWithKey(t)
	s.AddUser("222")
	s.AddUser("333")
	first := addProposalBytesForUser(t, s, key, "222")
	second := addProposalBytesForUser(t, s, key, "333")
	s.OnDaveMLSProposals(buildProposalBatch(first))
	s.OnDaveMLSProposals(buildProposalBatch(second))
	if len(s.commitCandidates) != 2 || s.pendingEpoch != nil {
		t.Fatal("candidates not isolated")
	}
	epoch, err := s.mlsClient.client.Epoch(context.Background(), s.groupID)
	if err != nil || epoch != 0 {
		t.Fatalf("epoch=%d err=%v", epoch, err)
	}
	s.OnDaveMLSPrepareCommitTransition(9, s.commitCandidates[1].CommitBytes())
	if s.pendingEpoch == nil || len(s.pendingEpoch.senders) != 3 {
		t.Fatal("selected candidate missing members")
	}
	s.OnDaveExecuteTransition(9)
	if s.activeEpoch == nil || len(s.activeEpoch.senders) != 3 {
		t.Fatal("selected epoch not active")
	}
}

func TestCandidateBatchFailureAndRevoke(t *testing.T) {
	s, key := setupActiveSoleMemberSessionWithKey(t)
	s.AddUser("222")
	s.AddUser("333")
	first := addProposalBytesForUser(t, s, key, "222")
	second := addProposalBytesForUser(t, s, key, "333")
	broken := mutateMLSMessage(t, second, func(msg *framing.MLSMessage) { pub, _ := msg.AsPublic(); pub.Auth.Signature.AsSlice()[0] ^= 1 })
	ctx := context.Background()
	gid := group.NewGroupID(s.groupID)
	before, err := s.mlsClient.store.LoadGroupState(ctx, gid)
	if err != nil {
		t.Fatal(err)
	}
	s.OnDaveMLSProposals(buildProposalBatch(append(append([]byte(nil), first...), broken...)))
	after, err := s.mlsClient.store.LoadGroupState(ctx, gid)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || len(s.commitCandidates) != 0 {
		t.Fatal("failed batch mutated state")
	}
	s.OnDaveMLSProposals(buildProposalBatch(first))
	s.OnDaveMLSProposals(buildProposalBatch(second))
	raw, err := s.mlsClient.store.LoadGroupState(ctx, gid)
	if err != nil {
		t.Fatal(err)
	}
	state, err := group.UnmarshalGroupState(raw)
	if err != nil {
		t.Fatal(err)
	}
	var refs []byte
	for _, p := range state.StoredProposals() {
		refs = append(refs, p.Ref...)
	}
	s.OnDaveMLSProposals(buildRevokeBatch(refs))
	if len(s.commitCandidates) != 0 || s.pendingEpoch != nil {
		t.Fatal("cancelled candidate survived")
	}
	raw, err = s.mlsClient.store.LoadGroupState(ctx, gid)
	if err != nil {
		t.Fatal(err)
	}
	state, err = group.UnmarshalGroupState(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.StoredProposals()) != 0 {
		t.Fatal("revoked proposals survived")
	}
}

func TestCandidateRejectsDisconnectedAdd(t *testing.T) {
	s, key := setupActiveSoleMemberSessionWithKey(t)
	s.AddUser("222")
	s.OnDaveMLSProposals(buildProposalBatch(addProposalBytesForUser(t, s, key, "222")))
	commit := s.commitCandidates[0].CommitBytes()
	s.RemoveUser("222")
	s.OnDaveMLSPrepareCommitTransition(9, commit)
	if s.Stats().CommitsFailed != 1 || s.pendingEpoch != nil || len(s.commitCandidates) != 0 {
		t.Fatal("stale Add was not rejected and cleared")
	}
}

func TestCandidateDoesNotSendDisconnectedCachedAdd(t *testing.T) {
	s, key := setupActiveSoleMemberSessionWithKey(t)
	s.AddUser("222")
	s.OnDaveMLSProposals(buildProposalBatch(addProposalBytesForUser(t, s, key, "222")))
	if len(s.commitCandidates) != 1 {
		t.Fatal("fixture candidate missing")
	}
	s.RemoveUser("222")
	// A new event may regenerate the cached proposal set after the user left.
	// Recheck membership before emitting another Commit, not only on echo.
	before := append([]byte(nil), s.pendingCommitBytes...)
	if err := s.prepareCommitCandidateLocked(); err == nil {
		t.Fatal("generated candidate for disconnected member")
	}
	if len(s.commitCandidates) != 1 || !bytes.Equal(before, s.pendingCommitBytes) {
		t.Fatal("failed preparation changed candidates")
	}
}

func TestDuplicateRemoveBatchIsAtomic(t *testing.T) {
	s, key := setupActiveSoleMemberSessionWithKey(t)
	s.AddUser("222")
	s.OnDaveMLSProposals(buildProposalBatch(addProposalBytesForUser(t, s, key, "222")))
	confirmLatestCandidate(t, s)
	s.OnDaveExecuteTransition(42)
	state, err := group.UnmarshalGroupState(currentGroupBytes(t, s))
	if err != nil {
		t.Fatal(err)
	}
	pm, err := framing.NewPublicMessage(framing.FramedContent{
		GroupID: s.groupID, Epoch: state.Epoch().AsUint64(), Sender: framing.Sender{Type: framing.SenderTypeExternal, SenderIndex: 0},
		Body: framing.ProposalBody{Data: group.ProposalMarshal(group.NewRemoveProposal(1))},
	}, ciphersuite.NewSignaturePrivateKey(key), nil, nil, state.CipherSuite())
	if err != nil {
		t.Fatal(err)
	}
	wire := framing.NewMLSMessagePublic(pm).Marshal()
	before := currentGroupBytes(t, s)
	if _, err = s.processProposalBatchLocked(buildProposalBatch(append(append([]byte(nil), wire...), wire...))); err == nil {
		t.Fatal("duplicate Remove accepted")
	}
	if !bytes.Equal(before, currentGroupBytes(t, s)) {
		t.Fatal("duplicate Remove partially updated state")
	}
	if _, err = s.processProposalBatchLocked(buildProposalBatch(wire)); err != nil {
		t.Fatalf("valid Remove rejected: %v", err)
	}
	state, err = group.UnmarshalGroupState(currentGroupBytes(t, s))
	if err != nil {
		t.Fatal(err)
	}
	if len(state.StoredProposals()) != 1 {
		t.Fatal("unexpected Remove cache")
	}
	if err = s.processRevokeProposalsLocked(buildRevokeBatch(state.StoredProposals()[0].Ref)[1:]); err != nil {
		t.Fatal(err)
	}
	state, err = group.UnmarshalGroupState(currentGroupBytes(t, s))
	if err != nil {
		t.Fatal(err)
	}
	if len(state.StoredProposals()) != 0 {
		t.Fatal("revoked duplicate Remove survived")
	}
}
