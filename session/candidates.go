package session

import (
	"bytes"
	"context"

	"github.com/thomas-vilte/mls-go"
	"github.com/thomas-vilte/mls-go/framing"
	"github.com/thomas-vilte/mls-go/group"
)

const maxCommitCandidates = 16

func (s *Session) clearCommitCandidatesLocked() {
	for _, candidate := range s.commitCandidates {
		candidate.Close()
	}
	s.commitCandidates = nil
	s.pendingCommitBytes = nil
}

func (s *Session) prepareCommitCandidateLocked() error {
	candidate, err := s.mlsClient.client.PrepareCommitCandidate(context.Background(), s.groupID, mls.CommitPendingProposalsOptions{GroupInfoOptions: []group.InfoOption{group.WithExternalPub(false)}})
	if err != nil {
		return err
	}
	commit, welcome := candidate.CommitBytes(), candidate.WelcomeBytes()
	if err := s.validateCandidateCoverageLocked(commit, true); err != nil {
		candidate.Close()
		return err
	}
	payload := append([]byte(nil), commit...)
	if len(welcome) > 0 {
		msg, e := framing.UnmarshalMLSMessage(welcome)
		if e != nil {
			candidate.Close()
			return e
		}
		if msg.Welcome == nil {
			candidate.Close()
			return ErrExpectedWelcome
		}
		payload = append(payload, msg.Welcome...)
	}
	if len(s.commitCandidates) >= maxCommitCandidates {
		s.commitCandidates[0].Close()
		s.commitCandidates = s.commitCandidates[1:]
	}
	s.commitCandidates = append(s.commitCandidates, candidate)
	s.pendingCommitBytes = append([]byte(nil), commit...)
	if s.callbacks != nil {
		if err = s.sendLocked(func() error { return s.callbacks.SendMLSCommitWelcome(payload) }); err != nil {
			s.commitCandidates = s.commitCandidates[:len(s.commitCandidates)-1]
			candidate.Close()
			s.pendingCommitBytes = nil
			if len(s.commitCandidates) > 0 {
				s.pendingCommitBytes = s.commitCandidates[len(s.commitCandidates)-1].CommitBytes()
			}
			s.watchDroppedSendLocked(err)
			return err
		}
	}
	return nil
}

func (s *Session) adoptOwnCandidateLocked(commit []byte) (bool, error) {
	for _, candidate := range s.commitCandidates {
		if !bytes.Equal(candidate.CommitBytes(), commit) {
			continue
		}
		if err := s.mlsClient.client.AdoptCommitCandidate(context.Background(), candidate); err != nil {
			return true, err
		}
		epoch, err := s.rebuildEpochStateLocked(s.groupID)
		if err != nil {
			return true, err
		}
		s.pendingEpoch = epoch
		s.pendingGroupID = append([]byte(nil), s.groupID...)
		return true, nil
	}
	return false, nil
}
