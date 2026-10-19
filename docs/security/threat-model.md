# reminal Threat Model

**Status:** current as of the version in this repository.
**Companion document:** [Security architecture](architecture.md), which describes
the mechanisms referenced here.

This document states what reminal defends against, what it does not, and what
residual risk remains after mitigation. It is written to be useful to a reviewer
who is trying to break it, not to reassure one who is not.

---

## Assets

Ranked by what an attacker gains from compromising them.

1. **Interactive control of the host.** The highest-value asset by a wide margin.
   A session is not read-only: it carries keystrokes into a live shell and
   synthetic input into live applications. Compromise means arbitrary code
   execution as the user.
2. **Session content.** Terminal output, screen and window contents, clipboard and
   file transfers. Frequently contains credentials, keys, and customer data.
3. **The session key.** 256-bit AES key; recovering it retroactively decrypts any
   recorded traffic from that agent run.
4. **Owner device keys.** Long-lived Ed25519 keys granting PIN-free access until
   revoked. The only credential that persists across sessions.
5. **Session metadata.** Which machines exist, when they are active, viewer counts.
   Low direct value, useful for targeting.

## Adversaries

### A1 — Passive network observer

*Capability:* records traffic between agent, relay, and viewer.

*Mitigations:* WSS/TLS on every hop. Beneath that, payloads are already sealed with
AES-256-GCM under a key the observer never sees. The key exchange is CPace, a
password-authenticated key exchange in which no recorded value's distribution
depends on the PIN, so **no offline brute-force is possible** — this is the
property `internal/crypto/cpace.go` is built to provide. Fresh scalars per
connection give forward secrecy: a PIN disclosed later does not decrypt traffic
recorded earlier.

*Residual risk:* traffic analysis. Frame sizes and timing leak typing cadence and
activity patterns. Not mitigated.

### A2 — Malicious or compromised relay operator

**This is the adversary the architecture is designed around**, and the reason the
public relay is acceptable to use.

*Capability:* sees all traffic, controls routing, can inject, drop, reorder, and
attempt to impersonate either party.

*Mitigations:*

- Payloads are opaque ciphertext; the relay holds no session key.
- Between ends that both support sealed frames, each message's type, direction,
  stream, and position are bound into its encryption (architecture §5.1). While
  a viewer from before sealed frames is attached, the machine also writes the
  earlier form, and that form has fewer of these properties; updating every
  viewer removes it.
- The offer of sealed frames, and the machine's answer to it, are bound into
  what each handshake authenticates (the PIN handshake's key, the owner
  handshake's signed transcripts), so a relay cannot strip them unseen. A
  viewer remembers the sessions and machines it has seen seal, and refuses an
  answer without sealed frames from them.
- A captured owner handshake replayed is not answered again within an hour of
  the first (the machine remembers the viewer keys it has answered for that
  long). One replayed later is answered, but gives no key — the reply is
  encrypted to an ephemeral key only the original device holds — and costs
  one handshake; a timestamp in the owner transcript would end even that.
- The relay performs **no PIN verification and never receives the PIN**. This is a
  deliberate capability refusal: a relay able to check a PIN could brute-force it
  offline and could take part in the exchange as either side
  (`internal/relay/auth.go` documents the reasoning).
- Active MITM against the PIN path costs one **online** handshake per guess, and a wrong
  guess fails the unwrap and drops the viewer — loud and slow (see [Brute-force
  economics](#brute-force-economics)).
- Against the owner path, MITM fails outright. The signed transcript binds both
  ephemerals and both identities, and the viewer verifies the agent's machine key
  against a **pinned** value. A relay substituting its own identity is detected and
  reported to the user as a machine-identity change.
- WebRTC signaling rides inside the encrypted channel, so the relay cannot swap
  DTLS fingerprints.
- No session content is stored, so there is no historical corpus to seize —
  compromising the relay yields future traffic only, and that traffic is ciphertext.

*Residual risk:* full metadata visibility. Denial of service — the relay can always
refuse to route. **Plaintext visibility into `reminal expose` traffic**, which is
not end-to-end encrypted (architecture §5.3). First-connection TOFU on the owner
path: a relay that is malicious at the very first connection between a device and a
machine can insert itself before any key is pinned, if the person confirms the
machine it presents. Two residuals of the earlier, unsealed form, both ending
when the machine stops writing it (with the switch that makes a viewer refuse
any answer without sealed frames): a viewer's **first** contact with a session
or machine it has never seen seal can still be dropped to the earlier form by
the relay; and a PIN handshake that offers no sealed frames makes the machine
write the earlier form beside the sealed one for 30 seconds before anyone has
proved the PIN, so a relay can keep that copy being written. It is encrypted
under the session key, a viewer that seals ignores it, and the machine takes
none of its own messages back as input; what it exposes is an earlier-form
viewer attached at the time.

### A3 — Attacker who knows the session ID but not the PIN

*Capability:* connects to the correct room and attempts handshakes.

*Mitigations:* the PIN selects the handshake's generator; a wrong PIN produces a
different wrap key and the unwrap fails. Guessing is strictly online — each
attempt requires a live handshake with the agent, and each handshake is exactly
one guess — and rate-limited in the agent (`internal/client/agent.go`) by a short-term allowance
(a burst of 8, one back per 10 seconds), a long-term one (a burst of 30, one back
per 3 minutes), and an escalating backoff for handshakes from sources that have
not proved the PIN: 30 of them, back at one an hour, then a wait before the next
that doubles from 3 minutes up to an hour, each quiet hour after a wait taking
one doubling off. That is about **24 sustained guesses per day**. Sources that
have proved the PIN skip the backoff; owner devices never use the PIN. The relay
names each handshake's source, so a relay guessing the PIN could name a proven
one; but a source keeps the exemption only while its handshakes go on proving
the PIN (at most 3 may be outstanding), and only a real viewer's proof earns it
back, one handshake each. That buys such a relay a few guesses per real
connection on top of the 24. The
trade-off: a stranger who keeps guessing can make a first-time PIN user wait up
to an hour, and the session's terminal is told when that starts.

*Residual risk:* see below.

#### Brute-force economics

| Scenario | Search space | At ~24 guesses/day |
|---|---|---|
| PIN only (session ID already known), 8 digits | 10⁸ | ~5,700 years expected |
| PIN only, a 6-digit PIN from before | 10⁶ | ~57 years expected |
| Session ID only | 32⁸ ≈ 1.1 × 10¹² | Infeasible |
| Both unknown | ~10¹⁸ | Infeasible |

The practical bound is not the arithmetic but the **session lifetime**. Credentials
exist only while the agent runs and are destroyed on exit, so a realistic session
lasting hours or days is not brute-forceable even in the favourable case where the
attacker already holds the session ID. A long-running unattended agent narrows that
margin; owner-device enrollment removes the PIN from the attack surface entirely
and is the recommended configuration for persistent agents.

> **Documentation accuracy note.** The relay's 5-strike PIN lockout was *removed*
> for session connections when the relay stopped seeing the PIN at all — it was
> replaced by the agent-side token bucket described above. The 5-attempt lockout
> that remains in `cloudflare/src/session.ts` applies only to the `reminal expose`
> PIN gate. Any statement that sessions are protected by a relay-enforced 5-strike
> lockout is stale and should be corrected.

### A4 — Malicious viewer (holds valid credentials)

*Capability:* full interactive control of the host.

*Mitigations:* none, by design. **A viewer that authenticates is the user** — the
same design decision SSH makes about anyone holding a valid key.

*Residual risk:* full host control, which is the intended behaviour rather than a
defect. The operational consequence is that credentials define the trust boundary,
so they should be handled accordingly:

- Transmit the session ID and PIN over **different channels**.
- `Ctrl+C` when finished; credentials die instantly and cannot be recovered.
- Treat owner-device enrollment as equivalent to adding an SSH key to
  `authorized_keys`, and audit the owner list.

In-session privilege separation — a read-only viewer, an input-disabled mode, or
per-viewer scope — is not built. Sessions intended for support or pair-debugging
should be scoped with that in mind.

### A5 — Local unprivileged attacker on the host

*Capability:* runs code as another user, or as the same user without root.

*Mitigations:* the authorised owner list is root-owned in `/etc/reminal`, so
enrollment requires `sudo` and an unprivileged process cannot add itself as an
owner. Device and machine keys are 0600. Revocation tombstones are agent-writable
and **override** the root-owned list, so a lost device can be revoked without
`sudo` and cannot be un-revoked by restoring a backup of `owners.json`.

*Residual risk:* an attacker running **as the same user** can read `~/.reminal/`,
including that device's owner key, and can read the running agent's memory. This is
not defended against; see [Non-goals](#non-goals).

### A6 — Supply chain

*Capability:* compromises the build pipeline, the GitHub release artifacts, or a
dependency, and ships a backdoored binary.

*Mitigations:* public AGPL source including the relay Worker, so both sides are
auditable and independently buildable. Releases built in CI from tagged commits.
macOS code signing. Atomic installs with rollback. A `critical_min` version floor
that force-upgrades every client within 24 hours when a security fix ships.

*Residual risk:* the updater relies on TLS to GitHub rather than verifying a
cryptographic signature over the downloaded archive. An attacker who had already
obtained GitHub release-publishing access could therefore ship a modified update,
and the `critical_min` mechanism that distributes fixes quickly would distribute it
at the same speed. This is common to auto-updating software generally, and the fix
is well understood: sign releases and verify client-side in `internal/updater`.

Of the adversaries listed here, this is the one with the most direct path to
compromise, which is why it is ranked first among the
[hardening opportunities](#hardening-opportunities).

### A7 — Cloudflare as an infrastructure insider

*Capability:* full access to Worker execution and Durable Object storage.

*Mitigations:* identical to A2 — the relay is untrusted by construction, so
Cloudflare's privileged position grants no access to session content. Stored state
is limited to routing and lockout metadata and is destroyed within 10 minutes of
agent disconnect.

*Residual risk:* metadata and timing. Compelled disclosure could yield connection
metadata but not session content, because no session content exists to disclose.
Self-hosting the relay removes Cloudflare from the trust chain for organizations
that require it.

### A8 — Attacker targeting an exposed port-forward

*Capability:* reaches the public `/p/<id>/` URL.

*Mitigations:* an unguessable session ID in the path; an optional bcrypt PIN gate
on a 12-character base32 PIN (60 bits), with a lockout per source and per port that doubles on each one (5 minutes, then 10, up to a day; a source that has got the PIN right before is held only by its own wrong PINs); a cookie per
visitor, an HMAC over a random visitor ID and when it was issued, which the relay
honours for 30 days at most, `HttpOnly` `Secure` `SameSite=Lax` and scoped to that
session's path; a per-registration signing key so a stale cookie from a reused
session ID grants nothing; and a 30-second upstream timeout.

*Residual risk:* the forwarded traffic is **plaintext to the relay** (architecture
§5.3). Anything the forwarded service exposes is exposed with it — reminal adds a
gate in front of the service, it does not make the service safe.

## Non-goals

reminal does not defend against these, and no configuration changes that.

- **An already-compromised host.** Root or same-user code execution on the machine
  defeats everything. reminal secures the path to a machine, not the machine.
- **A user who shares credentials with an attacker.** The session ID and PIN are
  designed to be human-transmissible; that is the feature.
- **A compromised or hostile viewer device.** A browser with malware sees whatever
  the user sees.
- **Denial of service.** A relay operator can always refuse to route. Volumetric
  attacks against the public relay are Cloudflare's concern.
- **Traffic analysis.** Frame timing and size are not padded or masked.
- **Post-compromise forensics.** Nothing is logged. This is the same design choice
  that means there is no session data to disclose, seize, or leak; the trade-off is
  that no record exists of who connected to what, when. Connection audit logging is
  on the roadmap for deployments that need it.

## Current limitations and roadmap

These fall into three distinct kinds, and conflating them would misrepresent all
three. **None is a known vulnerability.** Where an attack path exists it is stated
explicitly and linked to the adversary it derives from.

### Assurance not yet obtained

No known defect; independent verification has not been performed.

| Limitation | Relevant to | Path to closing |
|---|---|---|
| No third-party cryptographic review of `kex.go` / `owner.go` | Regulated buyers; anyone relying on the E2E claim without reading the source | Scoped, unfunded. The design rationale is documented inline for reviewers who wish to evaluate it directly |
| No SOC 2 / ISO 27001 / independent penetration test | Enterprise procurement | Deliberate sequencing — see [self-assessment](self-assessment.md#certifications-and-attestations) |

### Hardening opportunities

Real, with a stated attack path, and each with an identified fix.

| Limitation | Attack path | Status |
|---|---|---|
| Release archives are not signature-verified beyond TLS to GitHub | A6 — requires first compromising release publishing; the update channel would then reach the installed base within 24 hours | Fix identified: sign releases and verify client-side in `internal/updater` |
| `reminal expose` transits the relay in plaintext | A8 — relay operator can observe port-forwarded HTTP | Inherent to serving visitors who hold no reminal key. Self-host the relay for sensitive services |
| macOS builds are code-signed but not Apple-notarized | Not an attack path; affects Gatekeeper and MDM deployment | Requires an Apple Developer ID |
| Trust-on-first-use for a device's first owner connection | A2 — a relay malicious at that first connection could insert itself before a key is pinned | Inherent to TOFU. Out-of-band fingerprint verification would close it |
| Legacy bcrypt `pinHash` accepted for reattach alongside `token` | Weaker credential remains on the reattach path | Removable once older clients age out |
| No traffic padding | A1 — frame size and timing are observable | Not planned; the cost/benefit does not favour padding an interactive stream |

### Capabilities not built

Feature gaps rather than defects. These do not weaken a session; they limit where
reminal can be deployed under enterprise governance.

| Capability | Relevant to |
|---|---|
| Connection and owner-list audit logging | Enterprise deployment, incident review |
| SSO / SAML / SCIM | Enterprise identity governance |
| Read-only or input-disabled viewer role (A4) | Support and pair-debugging use cases |

## Deployment guidance

**Lowest risk.** Self-hosted relay, owner-device enrollment (no PIN in routine use),
sessions started on demand and terminated when done, `reminal expose` unused.

**Default.** Public relay, ephemeral session ID plus PIN sent over separate
channels, `Ctrl+C` when finished. Appropriate for personal machines and most
development work.

**Requires deliberate acceptance.** Long-running unattended agents (narrows the
brute-force margin — use owner devices instead); `reminal expose` on a relay you do
not operate (plaintext exposure); and regulated data in environments that require
independent assurance or connection audit logging, both of which are listed under
[current limitations](#current-limitations-and-roadmap).
