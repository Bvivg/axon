import Foundation

struct LiveChatAPI: ChatAPI {
    let client: any Axon_Chat_V1_ChatServiceClientInterface
    let connect: ConnectClient

    func rooms() async throws(APIError) -> [Room] {
        try await connect.call { headers in
            await client.listRooms(request: Axon_Chat_V1_ListRoomsRequest(), headers: headers)
        }.rooms
    }

    func room(id: String) async throws(APIError) -> RoomDetails {
        let request = Axon_Chat_V1_GetRoomRequest.with { $0.roomID = id }
        let response = try await connect.call { headers in
            await client.getRoom(request: request, headers: headers)
        }
        return RoomDetails(room: response.room, members: response.members)
    }

    func messages(in room: String, _ query: HistoryQuery) async throws(APIError) -> MessagePage {
        let request = Axon_Chat_V1_ListMessagesRequest.with {
            $0.roomID = room
            $0.limit = query.limit
            if let before = query.before { $0.beforeSeq = before }
            if let after = query.after { $0.afterSeq = after }
            $0.kinds = query.kinds
            if query.groupedItems { $0.includeGroupedItems = true }
        }
        let response = try await connect.call { headers in
            await client.listMessages(request: request, headers: headers)
        }
        return MessagePage(messages: response.messages, hasMore: response.hasMore_p)
    }

    func search(in room: String, for text: String) async throws(APIError) -> [ChatMessage] {
        let request = Axon_Chat_V1_SearchMessagesRequest.with {
            $0.roomID = room
            $0.query = text
        }
        return try await connect.call { headers in
            await client.searchMessages(request: request, headers: headers)
        }.messages
    }

    func markRead(room: String, seq: Int64) async throws(APIError) {
        let request = Axon_Chat_V1_MarkReadRequest.with {
            $0.roomID = room
            $0.seq = seq
        }
        _ = try await connect.call { headers in
            await client.markRead(request: request, headers: headers)
        }
    }

    func directRoom(with user: String) async throws(APIError) -> Room? {
        let request = Axon_Chat_V1_GetDirectRoomRequest.with { $0.userID = user }
        let response = try await connect.call { headers in
            await client.getDirectRoom(request: request, headers: headers)
        }
        return response.hasRoom ? response.room : nil
    }

    func createGroup(name: String, members: [String]) async throws(APIError) -> Room {
        let request = Axon_Chat_V1_CreateGroupRequest.with {
            $0.name = name
            $0.memberUserIds = members
        }
        return try await connect.call { headers in
            await client.createGroup(request: request, headers: headers)
        }.room
    }

    func addMembers(_ users: [String], to room: String) async throws(APIError) {
        let request = Axon_Chat_V1_AddGroupMembersRequest.with {
            $0.roomID = room
            $0.userIds = users
        }
        _ = try await connect.call { headers in
            await client.addGroupMembers(request: request, headers: headers)
        }
    }

    func removeMember(_ user: String, from room: String) async throws(APIError) {
        let request = Axon_Chat_V1_RemoveGroupMemberRequest.with {
            $0.roomID = room
            $0.userID = user
        }
        _ = try await connect.call { headers in
            await client.removeGroupMember(request: request, headers: headers)
        }
    }

    func renameGroup(_ room: String, to name: String) async throws(APIError) -> Room {
        let request = Axon_Chat_V1_RenameGroupRequest.with {
            $0.roomID = room
            $0.name = name
        }
        return try await connect.call { headers in
            await client.renameGroup(request: request, headers: headers)
        }.room
    }

    func leave(room: String) async throws(APIError) {
        let request = Axon_Chat_V1_LeaveRoomRequest.with { $0.roomID = room }
        _ = try await connect.call { headers in
            await client.leaveRoom(request: request, headers: headers)
        }
    }

    func hide(room: String) async throws(APIError) {
        let request = Axon_Chat_V1_HideRoomRequest.with { $0.roomID = room }
        _ = try await connect.call { headers in
            await client.hideRoom(request: request, headers: headers)
        }
    }

    func readers(of message: String) async throws(APIError) -> [MessageReader] {
        let request = Axon_Chat_V1_ListMessageReadersRequest.with { $0.messageID = message }
        return try await connect.call { headers in
            await client.listMessageReaders(request: request, headers: headers)
        }.readers
    }

    func forward(_ messages: [String], to rooms: [String], comment: String, clientID: String) async throws(APIError) -> Int {
        let request = Axon_Chat_V1_ForwardMessagesRequest.with {
            $0.messageIds = messages
            $0.roomIds = rooms
            $0.comment = comment
            $0.clientID = clientID
        }
        return try await Int(connect.call { headers in
            await client.forwardMessages(request: request, headers: headers)
        }.sent)
    }
}
