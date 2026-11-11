package session

import (
	"context"
	"encoding/binary"
	"github.com/thomas-vilte/mls-go"
	"github.com/thomas-vilte/mls-go/ciphersuite"
	"github.com/thomas-vilte/mls-go/credentials"
	"github.com/thomas-vilte/mls-go/framing"
	"github.com/thomas-vilte/mls-go/group"
	"github.com/thomas-vilte/mls-go/storage/memory"
	"strings"
	"testing"
)

func TestSignedCredentialChangingCommit(t *testing.T) {
	ctx := context.Background()
	cb := &kpCapturingCallbacks{}
	s := New("123456789", cb)
	closeAtCleanup(t, s)
	s.SetChannelID(42)
	s.OnSelectProtocolAck(1)
	external, key := buildExternalSenderPackageWithKey(t)
	s.OnDaveMLSExternalSenderPackage(external)
	store := memory.NewStore()
	peer, err := mls.NewClient(mustIdentity(t, "987654321"), ciphersuite.MLS128DHKEMP256, mls.WithStorage(store, store), mls.WithCacheStrategy(mls.CacheNone))
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	s.AddUser("987654321")
	kp, err := peer.FreshKeyPackageBytes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	gid, err := peer.CreateGroupWithExternalSender(ctx, binary.BigEndian.AppendUint64(nil, 42), kp, external)
	if err != nil {
		t.Fatal(err)
	}
	_, welcome, err := peer.InviteMember(ctx, gid, cb.lastKeyPackage())
	if err != nil {
		t.Fatal(err)
	}
	s.OnDaveMLSWelcome(7, welcome)
	s.OnDaveExecuteTransition(7)
	stageValidPeerAdd(t, s, peer, key, gid)
	raw, err := store.LoadGroupState(ctx, group.NewGroupID(gid))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(raw)
	malicious, err := group.UnmarshalGroupState(raw)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := store.LoadSignatureKey(ctx, group.NewGroupID(gid))
	if err != nil {
		t.Fatal(err)
	}
	// Model a member choosing a new credential before generating its UpdatePath.
	// Commit generates authentic leaf/frame signatures and confirmation data;
	// unlike a wire mutation, this fixture is cryptographically self-consistent.
	malicious.GetTreeLeaf(malicious.OwnLeafIndex()).LeafData.Credential = credentials.NewBasicCredentialFromUint64(666)
	malicious.IterateMembers(func(index group.LeafNodeIndex, member *group.Member) {
		if index == malicious.OwnLeafIndex() {
			member.Credential = credentials.NewBasicCredentialFromUint64(666)
		}
	})
	staged, err := malicious.Commit(signer, signer.PublicKey(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ac := staged.AuthenticatedContent()
	wire := framing.NewMLSMessagePublic(&framing.PublicMessage{Content: ac.Content, Auth: ac.Auth, MembershipTag: staged.MembershipTag()}).Marshal()
	if err := s.validateCommitCoverageLocked(wire); err == nil || !strings.Contains(err.Error(), "changes committer identity") {
		t.Fatalf("DAVE identity check: %v", err)
	}
	// Probe the underlying MLS receiver independently, without DAVE's check.
	// This intentionally changes the test-only receiver only after the check.
	err = s.mlsClient.client.ProcessCommit(ctx, gid, wire)
	if err != nil {
		t.Fatalf("signed fixture rejected before DAVE policy can be isolated: %v", err)
	}
	t.Log("MLS accepts the signed credential change; DAVE rejects it before adoption")
}
