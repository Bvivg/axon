//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/pkg/authn"
)

func (c *caller) updateProfile(accessToken string, req *authv1.UpdateProfileRequest) (*authv1.User, error) {
	r := connect.NewRequest(req)
	r.Header().Set(authn.Header, "Bearer "+accessToken)

	resp, err := c.client.UpdateProfile(context.Background(), r)
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetUser(), nil
}

func (c *caller) listSessions(accessToken string) ([]*authv1.Session, error) {
	r := connect.NewRequest(&authv1.ListSessionsRequest{})
	r.Header().Set(authn.Header, "Bearer "+accessToken)

	resp, err := c.client.ListSessions(context.Background(), r)
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetSessions(), nil
}

func (c *caller) revokeSession(accessToken, sessionID string) error {
	r := connect.NewRequest(&authv1.RevokeSessionRequest{SessionId: sessionID})
	r.Header().Set(authn.Header, "Bearer "+accessToken)

	_, err := c.client.RevokeSession(context.Background(), r)
	return err
}

func TestUpdateProfileChangesTakeEffectImmediately(t *testing.T) {
	c := newCaller(t)
	acct := c.register(t)

	first := "Ada"
	last := "Lovelace"

	updated, err := c.updateProfile(acct.tokens.GetAccessToken(), &authv1.UpdateProfileRequest{
		FirstName: &first,
		LastName:  &last,
	})
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if updated.GetDisplayName() != "Ada Lovelace" {
		t.Errorf("DisplayName = %q, want \"Ada Lovelace\"", updated.GetDisplayName())
	}

	user, err := c.getMe(acct.tokens.GetAccessToken())
	if err != nil {
		t.Fatalf("GetMe: %v", err)
	}
	if user.GetFirstName() != "Ada" || user.GetLastName() != "Lovelace" {
		t.Errorf("GetMe returned %q %q, want Ada Lovelace", user.GetFirstName(), user.GetLastName())
	}
}

func TestUpdateProfileEmailChangeResetsVerification(t *testing.T) {
	c := newCaller(t)
	acct := c.register(t)

	newEmail := "renamed-" + uuid.NewString() + "@axon.test"
	updated, err := c.updateProfile(acct.tokens.GetAccessToken(), &authv1.UpdateProfileRequest{
		Email: &newEmail,
	})
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if updated.GetEmail() != newEmail {
		t.Errorf("Email = %q, want %q", updated.GetEmail(), newEmail)
	}
	if updated.GetEmailVerified() {
		t.Error("EmailVerified stayed true across an email change")
	}
}

func TestListSessionsShowsEveryActiveSignIn(t *testing.T) {
	c := newCaller(t)
	acct := c.register(t)

	secondTokens, err := c.login(acct.email, testPassword)
	if err != nil {
		t.Fatalf("second login: %v", err)
	}

	sessions, err := c.listSessions(acct.tokens.GetAccessToken())
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(sessions))
	}

	var currentCount int
	for _, s := range sessions {
		if s.GetCurrent() {
			currentCount++
		}
		if s.GetUserAgent() == "" {
			t.Error("a session was recorded without a user agent")
		}
	}
	if currentCount != 1 {
		t.Errorf("%d sessions marked current, want exactly 1", currentCount)
	}

	sessionsFromSecond, err := c.listSessions(secondTokens.GetAccessToken())
	if err != nil {
		t.Fatalf("ListSessions from the second token: %v", err)
	}
	var secondCurrent string
	for _, s := range sessionsFromSecond {
		if s.GetCurrent() {
			secondCurrent = s.GetId()
		}
	}
	if secondCurrent == "" {
		t.Fatal("the second sign-in's own session was never marked current")
	}
}

func TestRevokeSessionEndsOnlyThatDevice(t *testing.T) {
	c := newCaller(t)
	acct := c.register(t)

	second, err := c.login(acct.email, testPassword)
	if err != nil {
		t.Fatalf("second login: %v", err)
	}

	sessions, err := c.listSessions(acct.tokens.GetAccessToken())
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}

	var otherID string
	for _, s := range sessions {
		if !s.GetCurrent() {
			otherID = s.GetId()
		}
	}
	if otherID == "" {
		t.Fatal("could not find the other session to revoke")
	}

	if err := c.revokeSession(acct.tokens.GetAccessToken(), otherID); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}

	if _, err := c.refresh(second.GetRefreshToken()); err == nil {
		t.Error("the revoked session's refresh token still works")
	}

	if _, err := c.getMe(acct.tokens.GetAccessToken()); err != nil {
		t.Errorf("the untouched session stopped working: %v", err)
	}
}

func TestRevokeSessionCannotTargetAnotherUser(t *testing.T) {
	c := newCaller(t)
	victim := c.register(t)
	attacker := c.register(t)

	sessions, err := c.listSessions(victim.tokens.GetAccessToken())
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	victimSessionID := sessions[0].GetId()

	err = c.revokeSession(attacker.tokens.GetAccessToken(), victimSessionID)
	requireCode(t, err, connect.CodeNotFound)

	if _, err := c.getMe(victim.tokens.GetAccessToken()); err != nil {
		t.Errorf("the victim's session was disturbed by another user's request: %v", err)
	}
}

func TestAvatarUploadIsResizedAndPublic(t *testing.T) {
	c := newCaller(t)
	acct := c.register(t)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, gatewayURL+"/api/avatar",
		bytes.NewReader(testPNG(t, 300, 300)))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+acct.tokens.GetAccessToken())

	uploadResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload avatar: %v", err)
	}
	defer func() { _ = uploadResp.Body.Close() }()

	if uploadResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(uploadResp.Body)
		t.Fatalf("upload avatar: status %d: %s", uploadResp.StatusCode, body)
	}

	var uploaded struct {
		AvatarURL  string `json:"avatar_url"`
		AvatarURLs struct {
			Small, Medium, Large, Original string
		} `json:"avatar_urls"`
	}
	if err := json.NewDecoder(uploadResp.Body).Decode(&uploaded); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if uploaded.AvatarURLs.Small == "" || uploaded.AvatarURLs.Medium == "" ||
		uploaded.AvatarURLs.Large == "" || uploaded.AvatarURLs.Original == "" {
		t.Fatalf("upload response is missing a size: %+v", uploaded)
	}

	fetchReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, uploaded.AvatarURLs.Small, nil)
	if err != nil {
		t.Fatalf("build fetch request: %v", err)
	}
	fetched, err := http.DefaultClient.Do(fetchReq)
	if err != nil {
		t.Fatalf("fetch the small avatar publicly: %v", err)
	}
	defer func() { _ = fetched.Body.Close() }()
	if fetched.StatusCode != http.StatusOK {
		t.Errorf("public fetch of the small avatar: status %d", fetched.StatusCode)
	}

	user, err := c.getMe(acct.tokens.GetAccessToken())
	if err != nil {
		t.Fatalf("GetMe: %v", err)
	}
	if user.GetAvatarUrls().GetMedium() != uploaded.AvatarURLs.Medium {
		t.Errorf("GetMe avatar_urls.medium = %q, want %q", user.GetAvatarUrls().GetMedium(), uploaded.AvatarURLs.Medium)
	}
}

func TestOAuthSignInImportsTheProviderAvatar(t *testing.T) {
	c := newCaller(t)

	sourceURL := stageTestImageInMinio(t)

	subject := "e2e-avatar-" + uuid.NewString()
	address := subject + "@axon.test"

	result, err := c.signInWith(t, url.Values{
		"sub":        {subject},
		"email":      {address},
		"avatar_url": {sourceURL},
	})
	if err != nil {
		t.Fatalf("sign in with an avatar_url: %v", err)
	}

	if result.GetUser().GetAvatarUrls() == nil {
		t.Fatal("the signed-in user has no avatar_urls; the provider avatar was not imported")
	}

	fetchReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		result.GetUser().GetAvatarUrls().GetMedium(), nil)
	if err != nil {
		t.Fatalf("build fetch request: %v", err)
	}
	fetched, err := http.DefaultClient.Do(fetchReq)
	if err != nil {
		t.Fatalf("fetch the imported avatar publicly: %v", err)
	}
	defer func() { _ = fetched.Body.Close() }()
	if fetched.StatusCode != http.StatusOK {
		t.Errorf("public fetch of the imported avatar: status %d", fetched.StatusCode)
	}
}

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.NRGBA{R: uint8(x % 256), G: uint8(y % 256), B: 90, A: 255})
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	return buf.Bytes()
}

func stageTestImageInMinio(t *testing.T) string {
	t.Helper()

	endpoint := os.Getenv("MINIO_ENDPOINT")
	if endpoint == "" {
		t.Skip("MINIO_ENDPOINT is not set; run via make test-e2e")
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds: credentials.NewStaticV4(os.Getenv("MINIO_ROOT_USER"), os.Getenv("MINIO_ROOT_PASSWORD"), ""),
	})
	if err != nil {
		t.Fatalf("minio client: %v", err)
	}

	bucket := os.Getenv("AVATARS_BUCKET")
	key := "e2e-fixtures/" + uuid.NewString() + ".png"
	data := testPNG(t, 200, 200)

	_, err = client.PutObject(t.Context(), bucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: "image/png"})
	if err != nil {
		t.Fatalf("stage a test image in minio: %v", err)
	}

	return "http://" + endpoint + "/" + bucket + "/" + key
}
