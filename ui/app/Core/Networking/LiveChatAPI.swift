import Foundation

struct LiveChatAPI: ChatAPI {
    let client: any Axon_Chat_V1_ChatServiceClientInterface
    let connect: ConnectClient

    func rooms() async throws(APIError) -> [Room] {
        try await connect.call { headers in
            await client.listRooms(request: Axon_Chat_V1_ListRoomsRequest(), headers: headers)
        }.rooms
    }
}
