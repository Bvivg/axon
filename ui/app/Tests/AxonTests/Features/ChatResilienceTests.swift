import Foundation
import Testing
@testable import Axon

struct ChatTimelineRefreshTests {
    @Test func keepsMessagesNewerThanTheFirstPage() {
        var timeline = ChatTimeline()
        timeline.merge([ChatFixtures.message(9)])
        timeline.replace(with: [ChatFixtures.message(7), ChatFixtures.message(8)])
        #expect(timeline.messages.map(\.seq) == [7, 8, 9])
    }

    @Test func refreshAppliesEditsMadeWhileAway() {
        var timeline = ChatTimeline()
        timeline.merge([ChatFixtures.message(1), ChatFixtures.message(2)])
        var edited = ChatFixtures.message(2)
        edited.body = "edited"
        let changed = timeline.refresh(with: [ChatFixtures.message(1), edited, ChatFixtures.message(3)])
        #expect(changed)
        #expect(timeline.messages.map(\.body) == ["message 1", "edited", "message 3"])
        let unchanged = timeline.refresh(with: [ChatFixtures.message(1)])
        #expect(!unchanged)
    }
}

@MainActor
struct ChatsHubResilienceTests {
    private func hub(_ world: FakeWorld, alerts: RecordingAlerts? = nil) -> ChatsViewModel {
        ChatsViewModel(services: world.services, me: GameFixtures.me, alerts: alerts)
    }

    @Test func alertsOnceAndRefreshesAfterAReconnect() async {
        let world = FakeWorld()
        world.chat.result = .success([SocialFixtures.room("r1", name: "One", sent: 10, seq: 1)])
        let alerts = RecordingAlerts()
        let model = hub(world, alerts: alerts)
        let run = Task { await model.run() }
        defer { run.cancel() }
        #expect(await eventually { model.loaded })
        #expect(alerts.activations == 1)
        world.sockets.socket.emit(.state(.open))
        let hidden = ChatFixtures.message(5, room: "r9", body: "back")
        world.sockets.socket.emit(.roomAdded(room: "r9", message: hidden))
        world.sockets.socket.emit(.message(hidden))
        #expect(await eventually { alerts.posted.count == 1 && world.chat.roomLoads == 2 })
        let loads = world.chat.roomLoads
        world.sockets.socket.emit(.state(.connecting(attempt: 0)))
        world.sockets.socket.emit(.state(.open))
        #expect(await eventually { world.chat.roomLoads > loads })
        #expect(alerts.posted.count == 1)
    }

    @Test func keepsNewerPreviewsWhenTheListArrivesLate() async {
        let world = FakeWorld()
        world.chat.result = .success([SocialFixtures.room("r1", name: "One", sent: 10, seq: 1)])
        let model = hub(world)
        let run = Task { await model.run() }
        defer { run.cancel() }
        #expect(await eventually { model.loaded })
        world.sockets.socket.emit(.message(ChatFixtures.message(4, room: "r1", body: "fresh")))
        #expect(await eventually { model.room("r1")?.lastMessage.seq == 4 })
        await model.refresh()
        #expect(model.room("r1")?.lastMessage.body == "fresh")
        #expect(model.room("r1")?.unreadCount == 1)
    }

    @Test func countsUnreadWhileTheAppIsInTheBackground() async {
        let world = FakeWorld()
        world.chat.result = .success([SocialFixtures.room("r1", name: "One", sent: 10, seq: 1)])
        let alerts = RecordingAlerts()
        let model = hub(world, alerts: alerts)
        let run = Task { await model.run() }
        defer { run.cancel() }
        #expect(await eventually { model.loaded })
        model.activeRoom = "r1"
        model.sceneVisible = false
        world.sockets.socket.emit(.message(ChatFixtures.message(2, room: "r1")))
        #expect(await eventually { model.room("r1")?.unreadCount == 1 })
        #expect(await eventually { alerts.posted.count == 1 })
    }
}
