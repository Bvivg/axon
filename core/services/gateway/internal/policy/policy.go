package policy

type Tier int

const (
	TierStandard Tier = iota

	TierSensitive
	TierSearch
)

type Rule struct {
	Public bool
	Tier   Tier
}

var procedures = map[string]Rule{
	"/axon.game.v1.GameService/CreateSession":     {Public: false, Tier: TierSensitive},
	"/axon.game.v1.GameService/JoinSession":       {Public: false, Tier: TierStandard},
	"/axon.game.v1.GameService/GetSession":        {Public: false, Tier: TierStandard},
	"/axon.game.v1.GameService/ListSessions":      {Public: false, Tier: TierStandard},
	"/axon.game.v1.GameService/FindMatch":         {Public: false, Tier: TierSensitive},
	"/axon.game.v1.GameService/CancelMatch":       {Public: false, Tier: TierStandard},
	"/axon.game.v1.GameService/GetMatch":          {Public: false, Tier: TierStandard},
	"/axon.game.v1.GameService/GetRatings":        {Public: false, Tier: TierStandard},
	"/axon.game.v1.GameService/LeaveSession":      {Public: false, Tier: TierStandard},
	"/axon.game.v1.GameService/GetSessionHistory": {Public: false, Tier: TierSearch},
	"/axon.game.v1.GameService/ListLiveSessions":  {Public: false, Tier: TierStandard},

	"/axon.analysis.v1.AnalysisService/GetAnalysis": {Public: false, Tier: TierStandard},

	"/axon.tournament.v1.TournamentService/CreateTournament":       {Public: false, Tier: TierSensitive},
	"/axon.tournament.v1.TournamentService/StartTournament":        {Public: false, Tier: TierSensitive},
	"/axon.tournament.v1.TournamentService/RegisterForTournament":  {Public: false, Tier: TierStandard},
	"/axon.tournament.v1.TournamentService/WithdrawFromTournament": {Public: false, Tier: TierStandard},
	"/axon.tournament.v1.TournamentService/GetTournament":          {Public: false, Tier: TierStandard},
	"/axon.tournament.v1.TournamentService/ListTournaments":        {Public: false, Tier: TierStandard},
	"/axon.tournament.v1.TournamentService/CancelTournament":       {Public: false, Tier: TierStandard},

	"/axon.calls.v1.CallsService/RegisterEndpoint":        {Public: false, Tier: TierSensitive},
	"/axon.calls.v1.CallsService/StartCall":               {Public: false, Tier: TierSensitive},
	"/axon.calls.v1.CallsService/StartDirectCall":         {Public: false, Tier: TierSensitive},
	"/axon.calls.v1.CallsService/AcceptCall":              {Public: false, Tier: TierSensitive},
	"/axon.calls.v1.CallsService/DeclineCall":             {Public: false, Tier: TierStandard},
	"/axon.calls.v1.CallsService/CancelCall":              {Public: false, Tier: TierStandard},
	"/axon.calls.v1.CallsService/EndCall":                 {Public: false, Tier: TierStandard},
	"/axon.calls.v1.CallsService/ReportConnection":        {Public: false, Tier: TierStandard},
	"/axon.calls.v1.CallsService/AcknowledgeCleanup":      {Public: false, Tier: TierStandard},
	"/axon.calls.v1.CallsService/GetCall":                 {Public: false, Tier: TierStandard},
	"/axon.calls.v1.CallsService/GetActiveCall":           {Public: false, Tier: TierStandard},
	"/axon.calls.v1.CallsService/ListCalls":               {Public: false, Tier: TierStandard},
	"/axon.calls.v1.CallsService/GetMediaCredentials":     {Public: false, Tier: TierStandard},
	"/axon.calls.v1.CallsService/GetCallCapabilities":     {Public: false, Tier: TierStandard},
	"/axon.calls.v1.CallsService/StartGroupCall":          {Public: false, Tier: TierSensitive},
	"/axon.calls.v1.CallsService/StartGroupCallFromCall":  {Public: false, Tier: TierSensitive},
	"/axon.calls.v1.CallsService/InviteCallParticipant":   {Public: false, Tier: TierSensitive},
	"/axon.calls.v1.CallsService/LeaveCall":               {Public: false, Tier: TierStandard},
	"/axon.calls.v1.CallsService/RemoveCallParticipant":   {Public: false, Tier: TierSensitive},
	"/axon.calls.v1.CallsService/GetSfuCredentials":       {Public: false, Tier: TierStandard},
	"/axon.calls.v1.CallsService/RequestCallMigration":    {Public: false, Tier: TierStandard},
	"/axon.calls.v1.CallsService/ReportCallMigration":     {Public: false, Tier: TierStandard},
	"/axon.calls.v1.CallsService/GetMigrationCredentials": {Public: false, Tier: TierStandard},

	"/axon.auth.v1.AuthService/Register":      {Public: true, Tier: TierSensitive},
	"/axon.auth.v1.AuthService/Login":         {Public: true, Tier: TierSensitive},
	"/axon.auth.v1.AuthService/StartOAuth":    {Public: true, Tier: TierSensitive},
	"/axon.auth.v1.AuthService/CompleteOAuth": {Public: true, Tier: TierSensitive},

	"/axon.auth.v1.AuthService/RefreshToken": {Public: true, Tier: TierSensitive},

	"/axon.auth.v1.AuthService/Logout": {Public: true, Tier: TierStandard},

	"/axon.auth.v1.AuthService/GetMe": {Public: false, Tier: TierStandard},

	"/axon.auth.v1.AuthService/GetUserByEmail": {Public: false, Tier: TierStandard},

	"/axon.auth.v1.AuthService/ListRoles": {Public: false, Tier: TierStandard},

	"/axon.auth.v1.AuthService/AssignRole": {Public: false, Tier: TierSensitive},
	"/axon.auth.v1.AuthService/RevokeRole": {Public: false, Tier: TierSensitive},

	"/axon.auth.v1.AuthService/SearchUsers":                {Public: false, Tier: TierSearch},
	"/axon.auth.v1.AuthService/GetUsersPublicProfiles":     {Public: false, Tier: TierStandard},
	"/axon.auth.v1.AuthService/GetPublicProfileByUsername": {Public: false, Tier: TierStandard},
	"/axon.auth.v1.AuthService/UpdateLastSeen":             {Public: false, Tier: TierStandard},

	"/axon.chat.v1.ChatService/ListRooms":      {Public: false, Tier: TierStandard},
	"/axon.chat.v1.ChatService/GetRoom":        {Public: false, Tier: TierStandard},
	"/axon.chat.v1.ChatService/ListMessages":   {Public: false, Tier: TierStandard},
	"/axon.chat.v1.ChatService/JoinRoom":       {Public: false, Tier: TierStandard},
	"/axon.chat.v1.ChatService/LeaveRoom":      {Public: false, Tier: TierStandard},
	"/axon.chat.v1.ChatService/HideRoom":       {Public: false, Tier: TierStandard},
	"/axon.chat.v1.ChatService/GetDirectRoom":  {Public: false, Tier: TierStandard},
	"/axon.chat.v1.ChatService/MarkRead":       {Public: false, Tier: TierStandard},
	"/axon.chat.v1.ChatService/SearchMessages": {Public: false, Tier: TierSearch},

	"/axon.chat.v1.ChatService/RemoveGroupMember": {Public: false, Tier: TierStandard},
	"/axon.chat.v1.ChatService/RenameGroup":       {Public: false, Tier: TierStandard},

	"/axon.chat.v1.ChatService/ListMessageReaders": {Public: false, Tier: TierStandard},
	"/axon.chat.v1.ChatService/ForwardMessages":    {Public: false, Tier: TierSensitive},

	"/axon.chat.v1.ChatService/GetPushKey":      {Public: false, Tier: TierStandard},
	"/axon.chat.v1.ChatService/SubscribePush":   {Public: false, Tier: TierStandard},
	"/axon.chat.v1.ChatService/UnsubscribePush": {Public: false, Tier: TierStandard},

	"/axon.chat.v1.ChatService/CreateRoom":      {Public: false, Tier: TierSensitive},
	"/axon.chat.v1.ChatService/CreateGroup":     {Public: false, Tier: TierSensitive},
	"/axon.chat.v1.ChatService/AddGroupMembers": {Public: false, Tier: TierSensitive},
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
