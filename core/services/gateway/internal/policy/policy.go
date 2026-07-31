// Package policy says what the gateway allows for each procedure.
//
// The table is explicit and closed: a procedure nobody listed requires
// authentication and gets the strict rate limit. Adding an RPC to a contract and
// forgetting to touch this file should make it unreachable, not public — the
// failure mode of the other default is an endpoint that quietly serves anyone.
package policy

// Tier selects which rate limit applies.
type Tier int

const (
	// TierStandard is the ordinary budget: reads and everything cheap.
	TierStandard Tier = iota

	// TierSensitive is the tight budget for operations that are expensive or
	// worth brute-forcing. Sign-in and registration are the obvious ones: a
	// shared budget large enough for normal browsing is large enough to grind
	// through a password list.
	TierSensitive
)

// Rule is what the gateway enforces for one procedure.
type Rule struct {
	// Public means no access token is required. It does not mean unlimited:
	// public procedures are exactly the ones an anonymous caller can reach, so
	// they are the ones that need a rate limit most.
	Public bool
	Tier   Tier
}

// procedures are the full Connect procedure paths from the generated code.
var procedures = map[string]Rule{
	// Anonymous by necessity — there is no token yet.
	"/axon.auth.v1.AuthService/Register":      {Public: true, Tier: TierSensitive},
	"/axon.auth.v1.AuthService/Login":         {Public: true, Tier: TierSensitive},
	"/axon.auth.v1.AuthService/StartOAuth":    {Public: true, Tier: TierSensitive},
	"/axon.auth.v1.AuthService/CompleteOAuth": {Public: true, Tier: TierSensitive},

	// The refresh token is the credential, so this is public in the sense that
	// no access token is required. It stays on the tight budget: a stolen token
	// should not be usable to mint access tokens at speed.
	"/axon.auth.v1.AuthService/RefreshToken": {Public: true, Tier: TierSensitive},

	// Logout takes a refresh token too, and has to work when the access token
	// has already expired — which is exactly when people close a tab.
	"/axon.auth.v1.AuthService/Logout": {Public: true, Tier: TierStandard},

	"/axon.auth.v1.AuthService/GetMe": {Public: false, Tier: TierStandard},
}

// For returns the rule for a procedure. An unlisted procedure gets the closed
// default: authenticated, on the tight budget.
func For(procedure string) Rule {
	if rule, ok := procedures[procedure]; ok {
		return rule
	}
	return Rule{Public: false, Tier: TierSensitive}
}

// Procedures returns every procedure the policy names. Tests use it to assert
// the table has not drifted from the contract.
func Procedures() []string {
	out := make([]string, 0, len(procedures))
	for p := range procedures {
		out = append(out, p)
	}
	return out
}
