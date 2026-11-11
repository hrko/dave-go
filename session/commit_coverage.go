package session

import (
	"bytes"
	"context"
	"fmt"
	"github.com/thomas-vilte/mls-go/framing"
	"github.com/thomas-vilte/mls-go/group"
	"github.com/thomas-vilte/mls-go/treesync"
)

func (s *Session) validateCommitCoverageLocked(wire []byte) error {
	return s.validateCandidateCoverageLocked(wire, false)
}

// locallyGenerated is only set before sending a candidate built from the
// current MLS client. Incoming packets must use validateCommitCoverageLocked.
func (s *Session) validateCandidateCoverageLocked(wire []byte, locallyGenerated bool) error {
	if len(s.groupID) == 0 {
		return ErrNoActiveGroup
	}
	raw, err := s.mlsClient.store.LoadGroupState(context.Background(), group.NewGroupID(s.groupID))
	if err != nil {
		return err
	}
	defer clear(raw)
	current, err := group.UnmarshalGroupState(raw)
	if err != nil {
		return err
	}
	own := locallyGenerated
	for _, candidate := range s.commitCandidates {
		if bytes.Equal(wire, candidate.CommitBytes()) {
			own = true
			break
		}
	}
	if current.Epoch().AsUint64() == 0 && !own {
		return fmt.Errorf("unexpected foreign commit before group establishment")
	}
	msg, err := framing.UnmarshalMLSMessage(wire)
	if err != nil {
		return err
	}
	pub, ok := msg.AsPublic()
	if !ok || pub == nil {
		return fmt.Errorf("commit must be public")
	}
	data, ok := pub.Content.CommitData()
	if !ok {
		return fmt.Errorf("expected commit")
	}
	commit, err := group.UnmarshalCommit(data)
	if err != nil {
		return err
	}
	if pub.Content.Sender.Type != framing.SenderTypeMember {
		return fmt.Errorf("commit must have a member sender")
	}
	if commit.Path != nil {
		tree, err := treesync.UnmarshalTree(current.MarshalRatchetTree(), current.CipherSuite())
		if err != nil {
			return err
		}
		old := tree.GetLeaf(treesync.LeafIndex(pub.Content.Sender.LeafIndex))
		next := commit.Path.LeafNode
		if old == nil || old.LeafData == nil || next == nil || old.LeafData.Credential == nil || next.Credential == nil {
			return fmt.Errorf("commit has invalid committer leaf")
		}
		if !bytes.Equal(old.LeafData.Credential.Marshal(), next.Credential.Marshal()) || !bytes.Equal(old.LeafData.SigKeyBytes(), next.SigKeyBytes()) {
			return fmt.Errorf("commit changes committer identity")
		}
	}
	expected := map[string]bool{}
	for _, stored := range current.StoredProposals() {
		if expected[string(stored.Ref)] {
			return fmt.Errorf("duplicate cached proposal")
		}
		expected[string(stored.Ref)] = true
		p := stored.Proposal
		if p == nil {
			return fmt.Errorf("nil cached proposal")
		}
		if p.Type == group.ProposalTypeAdd {
			if p.Add == nil || p.Add.KeyPackage == nil || p.Add.KeyPackage.LeafNode == nil || p.Add.KeyPackage.LeafNode.Credential == nil {
				return fmt.Errorf("invalid cached Add")
			}
			user, _, e := identityBytesToUserID(p.Add.KeyPackage.LeafNode.Credential.Identity)
			if e != nil {
				return e
			}
			if _, ok := s.users[user]; !ok {
				return fmt.Errorf("cached Add targets disconnected user")
			}
		}
	}
	if len(expected) == 0 || len(commit.Proposals) != len(expected) {
		return fmt.Errorf("commit must cover nonempty cached proposal set")
	}
	for _, ref := range commit.Proposals {
		if ref.Proposal != nil || !expected[string(ref.ProposalRef)] {
			return fmt.Errorf("unexpected or repeated commit proposal")
		}
		delete(expected, string(ref.ProposalRef))
	}
	return nil
}
