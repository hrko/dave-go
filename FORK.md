# AsterinMusic maintenance fork

Base: thomas-vilte/dave-go `ffcbed03c2166770f138a806b7471cbc1f02009c`.
Merged upstream `912e81502610f95b12ae9706a2de5209c76b83dd` (PR #62):
the panic recovery test uses a deterministic JoinGroup hook, preserving this
fork's channel/member validation fixtures. Normal JoinGroup behavior is unchanged.
The original module path is preserved; consumers pin this repository with a Go
module `replace` directive. This fork is not an independent cryptographic audit.

## Protocol corrections

- Executing an epoch switches the send ratchet immediately, including member
  removals. Previous receive epochs keep their existing retention policy.
  Reset the send nonce only when different key material is established; keep
  the counter on same-key activation. Legacy reset bridging before execution
  remains upstream behavior; AsterinMusic gates sending while unready.
- Validate Welcome group ID against the voice channel and require every remote
  member to be registered through AddUser before accepting the Welcome.
- Decode revoke as a vector of length-prefixed ProposalRefs, checking lengths,
  trailing bytes and count before mutating MLS. Commit remaining cached proposals.
  Confirmed state and outbound candidates are now isolated; no speculative
  rollback or delayed Proposal queue remains. See DAVE-CANDIDATES-01 below.
- Acknowledge nonzero prepare_transition for an available encrypted epoch, as
  well as downgrade. Do not acknowledge an encrypted transition without keys.
- Expose KeyPackage send success/failure counters and non-secret handshake
  progress flags. A successful send means callback completion, not a server ACK.

## Validation and limits

`go test ./...` includes the original tests with valid channel/member fixtures,
the specification probes, real revoke/recommit cases, encrypted-frame key-switch
checks, malformed input checks and diagnostic tests. Local peers alone do not
prove Discord or official libdave interoperability. Never log MLS messages,
KeyPackages, private state, credentials or key material in an integrator.

Concatenated append batches now use the deterministic public-Proposal prefix API
from hrko/mls-go `634fb81d9a189dcbf4eed3b8148e816b5a9767f0`, based on unreleased
upstream `20ed0bb`. The exact received bytes are passed to MLS authentication;
re-marshalling is not used to infer a message boundary. Entire-batch framing is
checked before staging any proposals; invalid outer lengths and truncated later
messages are rejected. Authentication/policy processing remains sequential, not
  an all-or-nothing transaction for otherwise well-framed messages in the old
  fork. The current work uses MLS-BATCH-01 to make validation atomic.

Consumers MUST repeat the mls-go replace directive in their main module: Go does
not inherit this module's replacements. Tests cover both joiners in the resulting
pending epoch, revoke/recommit, bad signatures, and unchanged state on malformed
batch framing. Official libdave 1.1.0/cpp and this fork both produced commit/welcome
for the same two-Add batch in an integration test. The existing common-Welcome
media interoperability tests also pass; this is not full competing-commit/revoke
differential coverage or proof that the live readiness timeout is resolved.

The new upstream MLS base passes the DAVE suite (including shuffled repeats).
Its Windows file-store directory Sync tests fail on unmodified upstream as well;
AsterinMusic uses the memory store. See the MLS fork's FORK.md for scope/limits.

Keep changes small and accompanied by protocol references and regression tests.
Do not replace MLS cryptography as part of state-management cleanup. If further
changes require broad restructuring, separate event transitions from I/O before
adding more recovery heuristics. Do not introduce blind KeyPackage resend loops.

## Current delta ledger

Reference: libdave `b5463c443df74db75a713c775b09a847621e6d62`, especially
`cpp/src/session.cpp` ProcessProposals/ProcessCommit/SetExternalSender and media
encryptor/decryptor transitions; https://daveprotocol.com/ MLS and media flows.
Binary interoperability separately uses official **1.1.0/cpp**, not that source
revision. Status of all entries: fork-only, no upstream PR, deployment pending.
On upstream updates classify every ID as retained, replaced or removed. Record
the upstream and removal commits and rerun the listed regressions. A clean merge
alone is not evidence that a delta can be removed.

| ID | Delta, rationale and implementation | Regression and removal condition |
| --- | --- | --- |
| DAVE-CANDIDATES-01 | `candidates.go`, `mls.go`, `session.go`, `close.go`: separate confirmed MLS, cached proposals, outbound candidates and media activation. Generation does not advance the group. Keep at most 16 candidates, evict oldest as libdave does; reset/adoption/Welcome/Close dispose them. Existing recovery watchdog bounds stalled handshakes. Requires MLS-CANDIDATE-01. | Candidate isolation, competing Commit, bounded queue, send failure and Close tests. Remove when upstream can select isolated Commit/Welcome candidates without speculative live-state changes and these tests pass. |
| DAVE-PROPOSALS-01 | `mls.go`: deterministic Proposal prefix parsing, complete batch validation, exact Revoke vectors, unknown/duplicate rejection; revoked proposals cannot revive. Requires MLS-PREFIX-01 and MLS-BATCH-01. Atomic validation follows libdave; it is not a claim about mandatory durable transactions. | Append/revoke/conformance and valid-prefix-invalid-signature tests. Remove when upstream preserves the cached set on every rejected batch and passes split/partial/full-revoke tests. |
| DAVE-COMMIT-01 | `commit_coverage.go`, `mls.go`: exact nonempty reference coverage; reject inline/unknown/repeated references, recheck Add recipients before generation and adoption, reject foreign Commit before establishment, require unchanged committer credential/signature key. Welcome validates channel and participants. Includes libdave client defenses beyond MLS authentication. | Candidate boundaries, disconnected Add, Commit and Welcome validation. `TestSignedCredentialChangingCommit` proves MLS accepts a correctly signed credential change while DAVE rejects it before adoption. Signature-key rotation is checked structurally; a fully signed rotation fixture remains pending. Remove only when upstream has equivalent checks and authenticated regressions pass. |
| DAVE-RESET-01 | `session.go`, `mls.go`: invalidation clears pending transition/candidates/readiness, drops send keys and expires old receive epochs. Established groups ignore External Sender replacement; unestablished groups rebuild on change. Preserve bounded recovery/degraded guards. | Reset, External Sender, readiness, move and shutdown tests. Remove when upstream has equivalent lifecycle behavior. |
| DAVE-KEYS-01 | `mediakeys/ratchet.go`, `mls.go`, `session.go`: Execute switches sending immediately; receive retention expires after 10 seconds including downgrade. Immutable base-secret SHA-256 identity selects nonce reset without old generation derivation. Never log this identity. | Same/new-domain, discarded ratchet, synthetic generation 257, downgrade and retention tests. Remove when upstream preserves nonce uniqueness and receive expiry across these transitions. |
| DAVE-DIAGNOSTICS-01 | Non-secret KeyPackage callback counters and handshake progress. Callback success is not server acknowledgement. | Diagnostic and dropped-send tests. Remove when upstream supplies equivalent milestones or the consumer no longer needs them. |

`go test ./...` passes in the current fork. Consumer interop now additionally
selects either Go or official Commit after batch Add, split Add, and split Add
followed by revoking the first proposal; each case verifies 100 new-epoch frames.
All six cases pass with local candidate changes; the previous fork fails the
split-input cases. Root integration and live Discord checks remain pending.
These synthetic tests do not establish the cause of historical readiness stalls.
MLS memory storage is used; durable-store multi-write atomicity remains outside
the new API guarantee, as recorded in the MLS fork's ledger. Consumer and interop
dependencies must be pinned to identical revisions at integration.

Current MLS dependency: hrko/mls-go `v1.6.1-0.20261111041611-caef2a26c624` (MLS-CANDIDATE-01 and MLS-BATCH-01).
