module github.com/go-fleettools/staletools

// ⛔ The PATCH, because `go 1.27` names none and govulncheck judges the
// standard library by the version the module claims. At `1.27` it answered for
// 1.27.1 and reported advisories whose fix is RELEASED in 1.27.2. None is
// reachable from here -- measured, not assumed -- but the lane below could not
// have said so either way, and this same line is the toolchain that lane runs,
// through `go-version-file: go.mod`.
//
// Within the same minor, so nothing that could build this before cannot now.
//
// Fitting, for the tool whose own job is to notice a reader built against an
// older Go than the source it reads.
//
// Raised again on 2026-10-25 for GO-2026-6809 — crypto/x509, where compromised
// intermediate certificates are no longer refused by Certificate.Verify — fix
// RELEASED in 1.27.3. Twice in a fortnight is the useful part: this line is not
// a floor chosen for the language features it admits. It is the toolchain the
// security gate judges, and it moves whenever a standard-library advisory is
// fixed.
//
// Raised to 1.27.4 on 2026-11-15 for GO-2026-6859 — net/http, fix RELEASED in
// 1.27.4. THIRD raise in three weeks, found by `modfloor --floor 1.27.4`
// reading the directive this module PUBLISHED, from the module proxy: eight of
// the eleven published modules of this organisation read BELOW at that moment.
//
// ⛔ RENOVATE WILL NEVER OFFER THIS UPDATE — measured on 2026-11-15, not
// assumed. Its github-actions manager maintains the CI pin (`with: go-version`,
// dependency `go`, type uses-with: go-fleettools/docs#29 is exactly that), and
// its gomod manager SEES this directive — the dependency dashboard lists it
// under "Detected Dependencies" — yet offers nothing for it. Of the three Go
// pins, the only one a consumer honours is the unwatched one. The gate for that
// belongs on a clock, as modfloor beside the nightly vulnscan sweep, and not in
// one more paragraph here.
go 1.27.4
