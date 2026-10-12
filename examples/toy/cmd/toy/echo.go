package main

import (
	"context"
	"errors"

	"goa.design/goa/v3/security"

	"github.com/xeger/goa-typescript/examples/toy/gen/echo"
	"github.com/xeger/goa-typescript/examples/toy/gen/types"
)

// echoService implements the echo service, which shares a method name and a
// type with the toy service.
type echoService struct {
	token string
}

func (s *echoService) JWTAuth(ctx context.Context, token string, _ *security.JWTScheme) (context.Context, error) {
	if token != s.token {
		return ctx, echo.MakeUnauthorized(errors.New("invalid token"))
	}
	return ctx, nil
}

func (s *echoService) StreamThingsSse(ctx context.Context, p *echo.StreamThingsSsePayload, stream echo.StreamThingsSseServerStream) error {
	return stream.SendWithContext(ctx, &types.ThingEvent{Type: "echo", ID: "echo-" + p.ID})
}
