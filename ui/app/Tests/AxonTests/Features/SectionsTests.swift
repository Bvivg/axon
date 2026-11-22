import Foundation
import SwiftProtobuf
import Testing
@testable import Axon

enum SocialFixtures {
    static func room(
        _ id: String,
        direct peer: String? = nil,
        name: String = "",
        sent: Int64 = 0,
        author: String = GameFixtures.rival,
        seq: Int64 = 1,
        read: Int64? = nil,
        unread: Int64 = 0
    ) -> Room {
        Room.with {
            $0.id = id
            $0.name = name
            $0.kind = peer == nil ? .group : .direct
            if let peer { $0.peerUserID = peer }
            $0.unreadCount = unread
            if let read { $0.othersReadSeq = read }
            $0.lastMessage = ChatMessage.with { message in
                message.id = "m-\(id)"
                message.authorID = author
                message.body = "hello \(id)"
                message.kind = .text
                message.seq = seq
                message.sentAt = Google_Protobuf_Timestamp(seconds: sent)
            }
        }
    }

    static func call(
        _ id: String,
        caller: String,
        peer: String,
        video: Bool = false,
        ended reason: String = "ended",
        started: Int64? = 100,
        ended: Int64? = 165
    ) -> Call {
        Call.with {
            $0.id = id
            $0.callerID = caller
            $0.mode = video ? .video : .audio
            $0.state = .ended
            $0.endReason = reason
            $0.createdAt = Google_Protobuf_Timestamp(seconds: 90)
            if let started { $0.startedAt = Google_Protobuf_Timestamp(seconds: started) }
            if let ended { $0.endedAt = Google_Protobuf_Timestamp(seconds: ended) }
            $0.participants = [caller, peer].map { user in
                Axon_Calls_V1_Participant.with { $0.userID = user }
            }
        }
    }
}

@MainActor
struct ChatsViewModelTests {
    @Test func listsRoomsNewestFirstWithPeerNames() async {
        let world = FakeWorld()
        world.chat.result = .success([
            SocialFixtures.room("old", name: "Club", sent: 10),
            SocialFixtures.room("new", direct: GameFixtures.rival, sent: 30),
            SocialFixtures.room("mid", name: "Team", sent: 20),
        ])
        world.auth.profileMap = [GameFixtures.rival: GameFixtures.profile(GameFixtures.rival, "grace")]
        let model = ChatsViewModel(services: world.services, me: GameFixtures.me)
        await model.refresh()
        #expect(model.rooms.map(\.id) == ["new", "mid", "old"])
        #expect(model.name(of: model.rooms[0]) == "grace")
        #expect(model.name(of: model.rooms[1]) == "Team")
        #expect(world.auth.profileRequests == [[GameFixtures.rival]])
        model.query = "tea"
        #expect(model.visible.map(\.id) == ["mid"])
    }

    @Test func loadsNamesOfPeopleInSystemPreviews() async {
        let world = FakeWorld()
        var group = SocialFixtures.room("g", name: "Team", sent: 10, author: GameFixtures.me)
        group.lastMessage.kind = .system
        group.lastMessage.system = Axon_Chat_V1_SystemPayload.with {
            $0.event = "member_removed"
            $0.actorID = GameFixtures.me
            $0.targetID = "legend"
        }
        world.chat.result = .success([group])
        world.auth.profileMap = ["legend": GameFixtures.profile("legend", "legend_1")]
        let model = ChatsViewModel(services: world.services, me: GameFixtures.me)
        await model.refresh()
        let preview = model.room("g")?.lastMessage.systemText(me: model.me, name: model.name(ofUser:))
        #expect(preview == "You removed legend_1")
    }

    @Test func marksOwnMessagesAsReadOnceOthersCaughtUp() async {
        let world = FakeWorld()
        let sent = SocialFixtures.room("a", direct: GameFixtures.rival, author: GameFixtures.me, seq: 5, read: 4)
        let read = SocialFixtures.room("b", direct: GameFixtures.rival, author: GameFixtures.me, seq: 5, read: 5)
        let model = ChatsViewModel(services: world.services, me: GameFixtures.me)
        #expect(model.isMine(sent.lastMessage))
        #expect(!model.isRead(sent))
        #expect(model.isRead(read))
    }

    @Test func reportsAnUnreachableServer() async {
        let world = FakeWorld()
        world.chat.result = .failure(APIError(.unavailable))
        let model = ChatsViewModel(services: world.services, me: GameFixtures.me)
        await model.refresh()
        #expect(model.error == "The server is unreachable.")
        #expect(model.loaded)
    }
}

@MainActor
struct CallsViewModelTests {
    @Test func describesDirectionOutcomeAndDuration() async {
        let world = FakeWorld()
        world.calls.result = .success([
            SocialFixtures.call("out", caller: GameFixtures.me, peer: GameFixtures.rival),
            SocialFixtures.call("missed", caller: GameFixtures.rival, peer: GameFixtures.me, video: true, ended: "missed", started: nil),
            SocialFixtures.call("unanswered", caller: GameFixtures.me, peer: GameFixtures.rival, ended: "missed", started: nil),
        ])
        world.auth.profileMap = [GameFixtures.rival: GameFixtures.profile(GameFixtures.rival, "grace")]
        let model = CallsViewModel(services: world.services, me: GameFixtures.me)
        await model.refresh()
        #expect(model.summary(of: model.calls[0]) == "Outgoing · Completed · 1:05")
        #expect(model.summary(of: model.calls[1]) == "Incoming · Missed")
        #expect(model.summary(of: model.calls[2]) == "Outgoing · No answer")
        #expect(model.name(of: model.calls[1]) == "grace")
    }

    @Test(arguments: [
        (CallFilter.all, ["out", "missed", "in"]),
        (CallFilter.missed, ["missed"]),
        (CallFilter.incoming, ["missed", "in"]),
        (CallFilter.outgoing, ["out"]),
        (CallFilter.video, ["missed"]),
        (CallFilter.audio, ["out", "in"]),
    ])
    func filtersHistory(filter: CallFilter, expected: [String]) async {
        let world = FakeWorld()
        world.calls.result = .success([
            SocialFixtures.call("out", caller: GameFixtures.me, peer: GameFixtures.rival),
            SocialFixtures.call("missed", caller: GameFixtures.rival, peer: GameFixtures.me, video: true, ended: "missed"),
            SocialFixtures.call("in", caller: GameFixtures.third, peer: GameFixtures.me),
        ])
        let model = CallsViewModel(services: world.services, me: GameFixtures.me)
        await model.refresh()
        model.filter = filter
        #expect(model.visible.map(\.id) == expected)
    }

    @Test func detailListsCallsWithTheSamePerson() async throws {
        let world = FakeWorld()
        world.calls.result = .success([
            SocialFixtures.call("out", caller: GameFixtures.me, peer: GameFixtures.rival),
            SocialFixtures.call("other", caller: GameFixtures.third, peer: GameFixtures.me),
            SocialFixtures.call("missed", caller: GameFixtures.rival, peer: GameFixtures.me, ended: "missed"),
        ])
        world.auth.profileMap = [GameFixtures.rival: GameFixtures.profile(GameFixtures.rival, "grace")]
        let model = CallsViewModel(services: world.services, me: GameFixtures.me)
        await model.refresh()
        let call = try #require(model.call("missed"))
        #expect(model.related(to: call).map(\.id) == ["out", "missed"])
        #expect(model.handle(of: call) == "@grace")
        #expect(model.call("gone") == nil)
    }
}

@MainActor
struct ProfileViewModelTests {
    private func user() -> User {
        User.with {
            $0.id = GameFixtures.me
            $0.email = "ada@axon.test"
            $0.username = "ada"
            $0.firstName = "Ada"
        }
    }

    @Test func sendsOnlyChangedFieldsAndAdoptsTheResult() async {
        let world = FakeWorld()
        var saved = user()
        saved.lastName = "Lovelace"
        world.auth.updateResult = .success(saved)
        var adopted: [User] = []
        let model = ProfileViewModel(services: world.services, user: user()) { adopted.append($0) }
        #expect(!model.canSave)
        model.lastName = "  Lovelace "
        #expect(model.canSave)
        await model.save()
        #expect(world.auth.updates == [ProfileChanges(lastName: "Lovelace")])
        #expect(model.saved)
        #expect(model.user.lastName == "Lovelace")
        #expect(adopted.map(\.lastName) == ["Lovelace"])
        #expect(!model.canSave)
    }

    @Test func uploadsAPhotoAsJPEGAndAdoptsTheNewAvatar() async throws {
        let world = FakeWorld()
        world.media.avatarResult = .success(AvatarUpload(
            url: "https://axon.test/a.jpg",
            sizes: AvatarUpload.Sizes(small: "s.jpg", medium: "https://axon.test/m.jpg", large: "l.jpg", original: "o.jpg")
        ))
        var adopted: [User] = []
        let model = ProfileViewModel(services: world.services, user: user()) { adopted.append($0) }
        await model.uploadPhoto(Self.png)
        #expect(model.photoError == nil)
        #expect(model.user.avatarLink == URL(string: "https://axon.test/m.jpg"))
        #expect(adopted.count == 1)
        await model.uploadPhoto(Data("not an image".utf8))
        #expect(model.photoError == "That file is not an image we can use.")
    }

    @Test func convertsPhotosToBoundedJPEG() throws {
        let jpeg = try #require(AvatarImage.jpeg(from: Self.png))
        #expect(jpeg.starts(with: [0xFF, 0xD8]))
    }

    private static let png = Data(
        base64Encoded: "iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAAFklEQVR4nGP8z8DAwMDAxMDAwMDAAAANHQEDasKb6QAAAABJRU5ErkJggg=="
    )!

    @Test func showsUsernameErrorsNextToTheField() async {
        let world = FakeWorld()
        world.auth.updateResult = .failure(APIError(.alreadyExists, "That username is taken."))
        let model = ProfileViewModel(services: world.services, user: user())
        model.username = "grace"
        await model.save()
        #expect(model.usernameError == "That username is taken.")
        #expect(model.saveError == nil)
    }

    @Test func endsAnotherSession() async {
        let world = FakeWorld()
        world.auth.sessionList = .success([
            SignInSession.with {
                $0.id = "here"
                $0.current = true
                $0.userAgent = "Axon/0.1.0 (iOS 27.0)"
            },
            SignInSession.with {
                $0.id = "there"
                $0.userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Chrome/140 Safari/537.36"
            },
        ])
        let model = ProfileViewModel(services: world.services, user: user())
        await model.loadSessions()
        #expect(model.current.map(\.device) == ["Axon app on iOS"])
        #expect(model.others.map(\.device) == ["Chrome on macOS"])
        await model.end(model.others[0])
        #expect(world.auth.revoked == ["there"])
        #expect(model.others.isEmpty)
        #expect(model.ended == "Session ended on Chrome on macOS.")
    }

    @Test func linksAProviderWithItsOwnVerifier() async throws {
        let world = FakeWorld()
        let start = OAuthStart(authorizationURL: try #require(URL(string: "https://github.com/login/oauth/authorize")), state: "s1")
        world.auth.linkStart = .success(start)
        world.auth.linkResult = .success(user())
        let model = ProfileViewModel(services: world.services, user: user())
        await model.connect(.github) { _ in URL(string: "axon://oauth/github?code=c1&state=s1") }
        #expect(model.linked == [.github])
        #expect(model.linkError == nil)
        let completion = try #require(world.auth.linkCompletions.first)
        #expect(completion.code == "c1")
        #expect(OAuthPKCE(verifier: completion.verifier).challenge == world.auth.linkChallenges.first)
    }

    @Test func refusesALinkCallbackForAnotherRequest() async throws {
        let world = FakeWorld()
        world.auth.linkStart = .success(OAuthStart(authorizationURL: try #require(URL(string: "https://github.com/x")), state: "mine"))
        let model = ProfileViewModel(services: world.services, user: user())
        await model.connect(.github) { _ in URL(string: "axon://oauth/github?code=c1&state=theirs") }
        #expect(world.auth.linkCompletions.isEmpty)
        #expect(model.linked.isEmpty)
        #expect(model.linkError != nil)
    }
}

@MainActor
struct LobbyRatingTests {
    @Test func loadsRatingsPerGameAndDefaultsTo1200() async {
        let world = FakeWorld()
        world.game.ratings[.chess] = Rating.with {
            $0.rating = 1350
            $0.games = 4
            $0.wins = 3
            $0.losses = 1
        }
        let model = LobbyViewModel(services: world.services, me: GameFixtures.me) { _ in }
        await model.refresh()
        #expect(model.rating(.chess) == 1350)
        #expect(model.rating(.quoridor) == 1200)
        #expect(model.ratings[.chess]?.record == "4 rated · 3W 1L 0D")
    }

    @Test func refetchesRatingsOnlyAfterAGameFinishes() async {
        let world = FakeWorld()
        let model = LobbyViewModel(services: world.services, me: GameFixtures.me) { _ in }
        await model.refresh()
        await model.refresh()
        #expect(world.game.ratingRequests.count == GameKind.allCases.count)
        world.game.mine = [GameFixtures.session(id: "done", status: .won, turn: nil, winner: GameFixtures.me)]
        await model.refresh()
        #expect(world.game.ratingRequests.count == GameKind.allCases.count * 2)
    }

    @Test func retriesRatingsThatFailedToLoad() async {
        let world = FakeWorld()
        world.game.ratingFailure = APIError(.unavailable)
        let model = LobbyViewModel(services: world.services, me: GameFixtures.me) { _ in }
        await model.refresh()
        world.game.ratingFailure = nil
        await model.refresh()
        #expect(world.game.ratingRequests.count == GameKind.allCases.count * 2)
        #expect(model.rating(.chess) == 1200)
    }
}

struct PreviewTextTests {
    @Test func previewsMessagesLikeTheWeb() {
        let voice = ChatMessage.with {
            $0.kind = .voice
            $0.voice = Axon_Chat_V1_VoicePayload.with { $0.durationMs = 65_000 }
        }
        let photo = ChatMessage.with {
            $0.kind = .image
            $0.body = ""
            $0.image = Axon_Chat_V1_ImagePayload()
        }
        let deleted = ChatMessage.with {
            $0.body = "gone"
            $0.deletedAt = Google_Protobuf_Timestamp(seconds: 1)
        }
        let renamed = ChatMessage.with {
            $0.kind = .system
            $0.body = "Club"
            $0.system = Axon_Chat_V1_SystemPayload.with { $0.event = "group_renamed" }
        }
        #expect(voice.preview == "🎤 Voice message · 1:05")
        #expect(photo.preview == "📷 Photo")
        #expect(deleted.preview == "Message deleted")
        #expect(renamed.preview == "Renamed to “Club”")
    }
}
