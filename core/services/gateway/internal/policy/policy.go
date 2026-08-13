package policy

type Tier int

const (
	TierStandard Tier = iota

	TierSensitive
)

type Rule struct {
	Public bool
	Tier   Tier
}

var procedures = map[string]Rule{

	"/axon.auth.v1.AuthService/Register":      {Public: true, Tier: TierSensitive},
	"/axon.auth.v1.AuthService/Login":         {Public: true, Tier: TierSensitive},
	"/axon.auth.v1.AuthService/StartOAuth":    {Public: true, Tier: TierSensitive},
	"/axon.auth.v1.AuthService/CompleteOAuth": {Public: true, Tier: TierSensitive},

	"/axon.auth.v1.AuthService/RefreshToken": {Public: true, Tier: TierSensitive},

	"/axon.auth.v1.AuthService/Logout": {Public: true, Tier: TierStandard},

	"/axon.auth.v1.AuthService/GetMe": {Public: false, Tier: TierStandard},

	"/axon.chat.v1.ChatService/ListRooms":    {Public: false, Tier: TierStandard},
	"/axon.chat.v1.ChatService/GetRoom":      {Public: false, Tier: TierStandard},
	"/axon.chat.v1.ChatService/ListMessages": {Public: false, Tier: TierStandard},
	"/axon.chat.v1.ChatService/JoinRoom":     {Public: false, Tier: TierStandard},
	"/axon.chat.v1.ChatService/LeaveRoom":    {Public: false, Tier: TierStandard},

	"/axon.chat.v1.ChatService/CreateRoom": {Public: false, Tier: TierSensitive},
}

func For(procedure string) Rule {
	if rule, ok := procedures[procedure]; ok {
		return rule
	}
	return Rule{Public: false, Tier: TierSensitive}
}

func Procedures() []string {
	out := make([]string, 0, len(procedures))
	for p := range procedures {
		out = append(out, p)
	}
	return out
}
