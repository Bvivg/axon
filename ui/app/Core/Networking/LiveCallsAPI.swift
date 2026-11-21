import Foundation

struct LiveCallsAPI: CallsAPI {
    let client: any Axon_Calls_V1_CallsServiceClientInterface
    let connect: ConnectClient

    func history(limit: Int) async throws(APIError) -> [Call] {
        var request = Axon_Calls_V1_ListCallsRequest()
        request.limit = Int32(limit)
        return try await connect.call { headers in
            await client.listCalls(request: request, headers: headers)
        }.calls
    }
}
