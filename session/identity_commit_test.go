package session

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/binary"
	"github.com/thomas-vilte/mls-go"
	"github.com/thomas-vilte/mls-go/ciphersuite"
	"github.com/thomas-vilte/mls-go/credentials"
	"github.com/thomas-vilte/mls-go/framing"
	"github.com/thomas-vilte/mls-go/group"
	"github.com/thomas-vilte/mls-go/schedule"
	"github.com/thomas-vilte/mls-go/storage/memory"
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

func TestSignedIdentityChangingCommit(t *testing.T) {
	for _, rotation := range []bool{false, true} {
		t.Run(map[bool]string{false: "credential", true: "signature-key"}[rotation], func(t *testing.T) { testSignedIdentityCommit(t, rotation) })
	}
}

func testSignedIdentityCommit(t *testing.T, rotation bool) {
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
	oldSigner := signer
	if rotation {
		newKey, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if e != nil {
			t.Fatal(e)
		}
		signer = ciphersuite.NewSignaturePrivateKey(newKey)
		leaf := malicious.GetTreeLeaf(malicious.OwnLeafIndex()).LeafData
		leaf.SignatureKey = &newKey.PublicKey
		leaf.SignatureKeyRaw = nil
	} else {
		malicious.GetTreeLeaf(malicious.OwnLeafIndex()).LeafData.Credential = credentials.NewBasicCredentialFromUint64(666)
		malicious.IterateMembers(func(index group.LeafNodeIndex, member *group.Member) {
			if index == malicious.OwnLeafIndex() {
				member.Credential = credentials.NewBasicCredentialFromUint64(666)
			}
		})
	}
	staged, err := malicious.Commit(signer, signer.PublicKey(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ac := staged.AuthenticatedContent()
	tag := staged.MembershipTag()
	if rotation {
		base, e := group.UnmarshalGroupState(raw)
		if e != nil {
			t.Fatal(e)
		}
		// Fixture-only access to the pinned MLS staging layout. Its public
		// RootPathSecret was consumed by HKDF; derive a fresh copy from the
		// preceding path secret before MergeCommit erases staging material.
		field := reflect.ValueOf(staged).Elem().FieldByName("pathSecrets")
		if !field.IsValid() || field.Type() != reflect.TypeOf([]*ciphersuite.Secret{}) {
			t.Fatal("MLS fixture layout changed")
		}
		paths := *(*[]*ciphersuite.Secret)(unsafe.Pointer(field.UnsafeAddr()))
		if len(paths) < 2 {
			t.Fatal("missing staged path")
		}
		commitSecret, e := paths[len(paths)-2].DeriveSecret(base.CipherSuite(), "path")
		if e != nil {
			t.Fatal(e)
		}
		if e = malicious.MergeCommit(staged); e != nil {
			t.Fatal(e)
		}
		// The new leaf is signed by its new key; the enclosing Commit is
		// authenticated by the current epoch's old key (RFC 9420).
		ac.GroupContext = base.GroupContext().Marshal()
		ac.Auth.Signature, e = ciphersuite.SignWithLabel(oldSigner, "FramedContentTBS", ac.MarshalTBS())
		if e != nil {
			t.Fatal(e)
		}
		hi, e := framing.NewConfirmedTranscriptHashInput(ac)
		if e != nil {
			t.Fatal(e)
		}
		hash, e := hi.Compute(base.CipherSuite(), base.InterimTranscriptHash())
		if e != nil {
			t.Fatal(e)
		}
		gc := *malicious.GroupContext()
		gc.ConfirmedTranscriptHash = hash
		ks := schedule.NewKeySchedule(base.CipherSuite(), base.EpochSecrets().InitSecret.Clone())
		ks.SetCommitSecret(commitSecret)
		if _, e = ks.ComputeJoinerSecret(gc.Marshal()); e != nil {
			t.Fatal(e)
		}
		if _, e = ks.ComputePskSecret(nil); e != nil {
			t.Fatal(e)
		}
		if _, e = ks.ComputeEpochSecret(gc.Marshal()); e != nil {
			t.Fatal(e)
		}
		secrets, e := ks.DeriveEpochSecrets()
		if e != nil {
			t.Fatal(e)
		}
		ac.Auth.ConfirmationTag = schedule.ComputeConfirmationTag(base.CipherSuite(), secrets.ConfirmationKey.AsSlice(), hash)
		tag = schedule.ComputeMembershipTag(base.CipherSuite(), base.EpochSecrets().MembershipKey.AsSlice(), ac.MarshalTBM())
	}
	wire := framing.NewMLSMessagePublic(&framing.PublicMessage{Content: ac.Content, Auth: ac.Auth, MembershipTag: tag}).Marshal()
	if err := s.validateCommitCoverageLocked(wire); err == nil || !strings.Contains(err.Error(), "changes committer identity") {
		t.Fatalf("DAVE identity check: %v", err)
	}
	// Probe the underlying MLS receiver independently, without DAVE's check.
	// This intentionally changes the test-only receiver only after the check.
	err = s.mlsClient.client.ProcessCommit(ctx, gid, wire)
	if err != nil {
		t.Fatalf("signed fixture rejected before DAVE policy can be isolated: %v", err)
	}
	t.Log("MLS accepts the authenticated identity change; DAVE rejects it before adoption")
}
