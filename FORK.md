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
  Revoke of an unconfirmed local commit restores its pre-commit cache first;
  confirmed transitions retain the existing ordered queue behavior.
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
an all-or-nothing transaction for otherwise well-framed messages.

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
