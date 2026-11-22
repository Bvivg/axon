import Foundation
import Testing
@testable import Axon

struct OAuthPKCETests {
    @Test func challengeIsTheBase64URLSHA256OfTheVerifier() {
        let pkce = OAuthPKCE(verifier: "dBjftJeZ4CVP-mJ92wcrwgHnEbVomMbQ9V3hG6PSU1A")
        #expect(pkce.challenge == "AK13oHBks1_90hG--DAPCVWNMFrrTPnG_rJzmsCxynw")
    }

    @Test func randomVerifiersAreLongEnoughAndDiffer() {
        let first = OAuthPKCE.random()
        let second = OAuthPKCE.random()
        #expect(first.verifier.count == 43)
        #expect(first.challenge.count == 43)
        #expect(first.verifier != second.verifier)
        #expect(!first.verifier.contains { "+/=".contains($0) })
    }
}

struct OAuthCallbackTests {
    @Test func readsTheCodeAndState() throws {
        let callback = try OAuthCallback(url: URL(string: "axon://oauth/github?code=c1&state=s1")!, provider: .github)
        #expect(callback.code == "c1")
        #expect(callback.state == "s1")
    }

    @Test(arguments: [
        ("https://oauth/github?code=c&state=s", APIError.Code.invalidArgument),
        ("axon://login/github?code=c&state=s", .invalidArgument),
        ("axon://oauth/google?code=c&state=s", .invalidArgument),
        ("axon://oauth/github?state=s", .invalidArgument),
        ("axon://oauth/github?error=access_denied", .cancelled),
        ("axon://oauth/github?error=server_error", .failedPrecondition),
    ])
    func refusesWhatIsNotAGitHubCallback(url: String, code: APIError.Code) {
        let error = #expect(throws: APIError.self) {
            try OAuthCallback(url: URL(string: url)!, provider: .github)
        }
        #expect(error?.code == code)
    }
}

@MainActor
struct AuthViewModelOAuthTests {
    private func started(_ auth: FakeAuth, state: String = "s1") {
        auth.oauthStart = .success(OAuthStart(authorizationURL: URL(string: "https://github.com/login/oauth/authorize")!, state: state))
    }

    @Test func signsInWithTheVerifierOfTheChallengeItSent() async throws {
        let auth = FakeAuth()
        started(auth)
        auth.oauthResult = .success(GameFixtures.user())
        let model = AuthViewModel(auth: auth)
        let user = await model.signIn(with: .github) { _ in URL(string: "axon://oauth/github?code=c1&state=s1")! }
        #expect(user?.id == GameFixtures.me)
        let completion = try #require(auth.oauthCompletions.first)
        #expect(completion.code == "c1")
        #expect(completion.state == "s1")
        #expect(OAuthPKCE(verifier: completion.verifier).challenge == auth.oauthChallenges.first)
        #expect(model.error == nil)
        #expect(model.signingInWith == nil)
    }

    @Test func aForeignStateIsNotCompleted() async {
        let auth = FakeAuth()
        started(auth)
        let model = AuthViewModel(auth: auth)
        let user = await model.signIn(with: .github) { _ in URL(string: "axon://oauth/github?code=c1&state=other")! }
        #expect(user == nil)
        #expect(auth.oauthCompletions.isEmpty)
        #expect(model.error == "The GitHub sign-in expired. Try again.")
    }

    @Test func closingTheSheetOrDenyingAccessIsSilent() async {
        let auth = FakeAuth()
        started(auth)
        let model = AuthViewModel(auth: auth)
        #expect(await model.signIn(with: .github) { _ in nil } == nil)
        #expect(await model.signIn(with: .github) { _ in URL(string: "axon://oauth/github?error=access_denied")! } == nil)
        #expect(model.error == nil)
        #expect(auth.oauthCompletions.isEmpty)
    }

    @Test func anUnreachableServerIsReported() async {
        let model = AuthViewModel(auth: FakeAuth())
        var presented = false
        let user = await model.signIn(with: .github) { _ in
            presented = true
            return nil
        }
        #expect(user == nil)
        #expect(!presented)
        #expect(model.error != nil)
    }
}
