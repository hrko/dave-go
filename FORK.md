# AsterinMusic maintenance fork

Base: thomas-vilte/dave-go `ffcbed03c2166770f138a806b7471cbc1f02009c`.
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

An additional dependency issue remains: mls-go v1.6.0 framing expects the end of
the input after the authentication tail and can reject a concatenated append
batch containing two valid Add proposals. The revoke tests seed that cache with
separate messages to test revoke independently. Fixing the MLS streaming decoder
and adding official multi-message differential tests is separate follow-up work;
do not claim all protocol defects or the live readiness timeout are resolved.

Keep changes small and accompanied by protocol references and regression tests.
Do not replace MLS cryptography as part of state-management cleanup. If further
changes require broad restructuring, separate event transitions from I/O before
adding more recovery heuristics. Do not introduce blind KeyPackage resend loops.
