package proxy

import (
	"context"

	"connectrpc.com/connect"

	analysisv1 "github.com/bvivg/axon/core/shared/gen/go/axon/analysis/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/analysis/v1/analysisv1connect"
)

type Analysis struct {
	client analysisv1connect.AnalysisServiceClient
}

var _ analysisv1connect.AnalysisServiceHandler = (*Analysis)(nil)

func NewAnalysis(client connect.HTTPClient, url string, opts ...connect.ClientOption) *Analysis {
	return &Analysis{client: analysisv1connect.NewAnalysisServiceClient(client, url, opts...)}
}

func (p *Analysis) GetAnalysis(ctx context.Context, req *connect.Request[analysisv1.GetAnalysisRequest]) (*connect.Response[analysisv1.GetAnalysisResponse], error) {
	return p.client.GetAnalysis(ctx, forward(ctx, req))
}
