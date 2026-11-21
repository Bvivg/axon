import Foundation

enum LiveServices {
    @MainActor
    static func make(settings: GatewaySettings, vault: TokenVault) -> Services {
        make(address: settings.address, deviceID: settings.deviceID, vault: vault)
    }

    static func make(
        address: GatewayAddress,
        deviceID: String,
        vault: TokenVault,
        sockets: any WebSocketTransport = URLSessionWebSocketTransport()
    ) -> Services {
        let transport = ConnectClient.transport(address: address, deviceID: deviceID)
        let auth = Axon_Auth_V1_AuthServiceClient(client: transport)
        let game = Axon_Game_V1_GameServiceClient(client: transport)
        let chat = Axon_Chat_V1_ChatServiceClient(client: transport)
        let calls = Axon_Calls_V1_CallsServiceClient(client: transport)
        let connect = ConnectClient(vault: vault)
        let refresher = TokenRefresher(client: auth)
        vault.setRefresher { refreshToken in await refresher.refresh(refreshToken) }
        return Services(
            auth: LiveAuthAPI(client: auth, vault: vault),
            game: LiveGameAPI(client: game, connect: connect),
            chat: LiveChatAPI(client: chat, connect: connect),
            calls: LiveCallsAPI(client: calls, connect: connect),
            channels: WebSocketGameChannelFactory(url: address.gameSocketURL, vault: vault, transport: sockets)
        )
    }
}
