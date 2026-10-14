package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"goa.design/goa/v3/security"

	"github.com/crossnokaye/goa-typescript/examples/toy/gen/toy"
	"github.com/crossnokaye/goa-typescript/examples/toy/gen/types"
)

// toyService implements the toy service with canned data. The TypeScript
// tests under ts/ assert on these exact values.
type toyService struct {
	token string
}

func (s *toyService) JWTAuth(ctx context.Context, token string, _ *security.JWTScheme) (context.Context, error) {
	if token != s.token {
		return ctx, toy.MakeUnauthorized(errors.New("invalid token"))
	}
	return ctx, nil
}

func (s *toyService) APIKeyAuth(ctx context.Context, key string, _ *security.APIKeyScheme) (context.Context, error) {
	if key != s.token {
		return ctx, toy.MakeUnauthorized(errors.New("invalid API key"))
	}
	return ctx, nil
}

func ptr[T any](v T) *T { return &v }

// Identifiers of the canned things.
const (
	fullThingID    = "11111111-1111-4111-8111-111111111111"
	parentThingID  = "22222222-2222-4222-8222-222222222222"
	minimalThingID = "33333333-3333-4333-8333-333333333333"
	squareThingID  = "44444444-4444-4444-8444-444444444444"
)

// fullThing sets every attribute.
func fullThing() *types.Thing {
	return &types.Thing{
		ID:      fullThingID,
		Name:    ptr("full-thing"),
		Kind:    "gadget",
		Enabled: true,
		// A non-UTC offset and nanoseconds, as Go clients routinely send.
		CreatedAt: ptr("2026-01-02T03:04:05.123456789-07:00"),
		BornOn:    ptr("2026-01-02"),
		Count:     ptr(int64(9007199254740991)),
		Small:     ptr(int32(-7)),
		Ratio:     ptr(0.25),
		Weight:    ptr(float32(1.5)),
		Blob:      []byte{0x00, 0xff, 'h', 'i'},
		Payload:   map[string]any{"nested": []any{1, "two", true, nil}},
		Tags:      []string{"a", "b"},
		Attrs:     map[string]int{"x": 1, "y": 2},
		Labels:    []*types.Label{{Key: "env", Value: "test"}},
		Parent: &types.Thing{
			ID:        parentThingID,
			Kind:      "widget",
			Enabled:   true,
			CreatedAt: ptr("2025-12-31T23:59:59Z"),
			Shape:     types.NewShapeSquare(&types.Square{Side: 3}),
		},
		Shape:      types.NewShapeCircle(&types.Circle{Radius: 2.5}),
		LegacyName: ptr("old-name"),
	}
}

// minimalThing sets only what the design requires or defaults. Its
// legacy_name is unset, which goa serializes as null because of the JSON tag
// override.
func minimalThing(id string) *types.Thing {
	return &types.Thing{ID: id, Kind: "widget", Enabled: true}
}

func (s *toyService) StreamThingsSse(ctx context.Context, p *toy.StreamThingsSsePayload, stream toy.StreamThingsSseServerStream) error {
	if p.ID == "missing" {
		return toy.MakeNotFound(fmt.Errorf("thing %q not found", p.ID))
	}
	square := minimalThing(squareThingID)
	square.Shape = types.NewShapeSquare(&types.Square{Side: 4})
	things := []*types.Thing{fullThing(), minimalThing(minimalThingID), square}
	for i, thing := range things {
		if i > 0 && p.DelayMs > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(p.DelayMs) * time.Millisecond):
			}
		}
		event := &types.ThingEvent{Type: "thing", ID: fmt.Sprintf("%s-%d", p.ID, i+1), Thing: thing}
		if err := stream.SendWithContext(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func (s *toyService) StreamWrappedSse(ctx context.Context, p *toy.StreamWrappedSsePayload, stream toy.StreamWrappedSseServerStream) error {
	after := 0
	if p.LastEventID != nil {
		n, err := strconv.Atoi(*p.LastEventID)
		if err != nil {
			return toy.MakePreconditionFailed(fmt.Errorf("last event ID %q is not a number", *p.LastEventID))
		}
		after = n
	}
	for i := after + 1; i <= p.Count; i++ {
		thing := minimalThing(minimalThingID)
		thing.Name = ptr(fmt.Sprintf("%s-%d", p.ID, i))
		thing.CreatedAt = p.Since
		envelope := &toy.ThingEventEnvelope{
			Data:  thing,
			ID:    ptr(strconv.Itoa(i)),
			Event: ptr("thing"),
		}
		if i == after+1 {
			envelope.Retry = ptr(1500)
		}
		if err := stream.SendWithContext(ctx, envelope); err != nil {
			return err
		}
	}
	return nil
}

func (s *toyService) StreamPostSse(ctx context.Context, p *toy.StreamPostSsePayload, stream toy.StreamPostSseServerStream) error {
	for i, kind := range p.Filter.Kinds {
		thing := minimalThing(minimalThingID)
		thing.Kind = kind
		thing.CreatedAt = p.Filter.Since
		event := &types.ThingEvent{Type: "filtered", ID: strconv.Itoa(i + 1), Thing: thing}
		if err := stream.SendWithContext(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func (s *toyService) StreamTextSse(ctx context.Context, _ *toy.StreamTextSsePayload, stream toy.StreamTextSseServerStream) error {
	for _, text := range []string{"line one\nline two", "single"} {
		if err := stream.SendWithContext(ctx, text); err != nil {
			return err
		}
	}
	return nil
}

func (s *toyService) StreamCountSse(ctx context.Context, _ *toy.StreamCountSsePayload, stream toy.StreamCountSseServerStream) error {
	for i := 1; i <= 3; i++ {
		if err := stream.SendWithContext(ctx, i); err != nil {
			return err
		}
	}
	return nil
}

func (s *toyService) StreamViewedSse(ctx context.Context, p *toy.StreamViewedSsePayload, stream toy.StreamViewedSseServerStream) error {
	stream.SetView(p.View)
	return stream.SendWithContext(ctx, &toy.ThingWithViews{ID: "viewed", Name: "viewed-thing", Secret: "s3cr3t"})
}

func (s *toyService) StreamPublicSse(ctx context.Context, stream toy.StreamPublicSseServerStream) error {
	return stream.SendWithContext(ctx, &types.ThingEvent{
		Type:     "public",
		ID:       "public-1",
		AuthSeen: ptr(authorizationSeen(ctx)),
	})
}

func (s *toyService) StreamKeyedSse(ctx context.Context, _ *toy.StreamKeyedSsePayload, stream toy.StreamKeyedSseServerStream) error {
	return stream.SendWithContext(ctx, &types.ThingEvent{Type: "keyed", ID: "keyed-1"})
}

func (s *toyService) GetThing(_ context.Context, p *toy.GetThingPayload) (*toy.ThingReply, error) {
	if p.ID == "missing" {
		return nil, toy.MakeNotFound(fmt.Errorf("thing %q not found", p.ID))
	}
	return &toy.ThingReply{
		Thing:   fullThing(),
		Etag:    `"` + p.ID + `-v1"`,
		Tags:    p.Tags,
		Verbose: p.Verbose,
		Since:   p.Since,
		Trace:   p.Trace,
	}, nil
}

func (s *toyService) GetThingViewed(_ context.Context, p *toy.GetThingViewedPayload) (*toy.ThingWithViews, string, error) {
	return &toy.ThingWithViews{ID: "viewed", Name: "viewed-thing", Secret: "s3cr3t"}, p.View, nil
}

func (s *toyService) ListThings(context.Context, *toy.ListThingsPayload) (toy.ThingWithViewsCollection, string, error) {
	return toy.ThingWithViewsCollection{
		{ID: "one", Name: "first", Secret: "s3cr3t"},
		{ID: "two", Name: "second", Secret: "s3cr3t"},
	}, "default", nil
}

func (s *toyService) CreateThing(_ context.Context, p *toy.CreateThingPayload) (*types.Thing, error) {
	switch p.Name {
	case "bad":
		return nil, &toy.ValidationProblem{Field: "name", Message: "bad is not a good name", RetryAfter: ptr(30)}
	case "taken":
		return nil, &toy.Conflict{ExistingID: fullThingID, Since: ptr("2026-01-02T03:04:05Z")}
	}
	thing := minimalThing(minimalThingID)
	thing.Name = ptr(p.Name)
	thing.Kind = p.Kind
	return thing, nil
}

func (s *toyService) EchoThing(_ context.Context, p *toy.EchoThingPayload) (*types.Thing, error) {
	// goa's request decoder has already filled in the defaults of the nested
	// thing (kind, enabled): what comes back is what the server saw.
	return p.Thing, nil
}

func (s *toyService) UpsertThing(_ context.Context, p *toy.UpsertThingPayload) (*toy.UpsertResult, error) {
	thing := minimalThing(minimalThingID)
	thing.Name = ptr(p.Name)
	if p.ID == "new" {
		// The tag selects the 201 response, which leaves the outcome out of the body.
		return &toy.UpsertResult{Thing: thing, Outcome: ptr("created")}, nil
	}
	return &toy.UpsertResult{Thing: thing, Outcome: ptr("updated")}, nil
}

func (s *toyService) FindThing(_ context.Context, p *toy.FindThingPayload) (*toy.FindThingResult, error) {
	if p.Name == "nothing" {
		// No tag: the untagged 204 response is used.
		return &toy.FindThingResult{}, nil
	}
	return &toy.FindThingResult{
		Match: &toy.ThingMatch{ID: fullThingID, Name: p.Name, FoundAt: ptr("2026-01-02T03:04:05Z")},
		Found: ptr("yes"),
	}, nil
}

func (s *toyService) DeleteThing(_ context.Context, p *toy.DeleteThingPayload) error {
	if p.ID == "missing" {
		return toy.MakeNotFound(fmt.Errorf("thing %q not found", p.ID))
	}
	return nil
}

func (s *toyService) DownloadThing(_ context.Context, p *toy.DownloadThingPayload) (*toy.DownloadThingResult, io.ReadCloser, error) {
	if p.ID == "missing" {
		return nil, nil, toy.MakeNotFound(fmt.Errorf("thing %q not found", p.ID))
	}
	// Not text: the client must hand the bytes over untouched.
	content := append([]byte{0x00, 0xff}, []byte("content of "+p.ID)...)
	return &toy.DownloadThingResult{
		ContentLength: int64(len(content)),
		ContentType:   "application/octet-stream",
	}, io.NopCloser(bytes.NewReader(content)), nil
}

func (s *toyService) UploadThing(_ context.Context, p *toy.UploadThingPayload, body io.ReadCloser) (*toy.UploadThingResult, error) {
	defer body.Close()
	content, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	return &toy.UploadThingResult{Size: len(content), ContentType: p.ContentType, Text: string(content)}, nil
}

func (s *toyService) StreamOrList(ctx context.Context, p *toy.StreamOrListPayload, stream toy.StreamOrListServerStream) ([]*toy.ThingSummary, error) {
	// goa streams when Accept names text/event-stream and answers with the
	// result otherwise. The generated client must ask for one or the other.
	if accept := acceptSeen(ctx); accept != "application/json" && accept != "text/event-stream" {
		return nil, toy.MakePreconditionFailed(fmt.Errorf("unexpected Accept header %q", accept))
	}
	if p.Empty != nil && *p.Empty {
		return nil, nil
	}
	things := []*toy.ThingSummary{
		{ID: "one", CreatedAt: ptr("2026-01-02T03:04:05Z")},
		{ID: "two"},
	}
	for _, thing := range things {
		// In the plain mode goa hands over a stream that discards events.
		if err := stream.SendWithContext(ctx, thing); err != nil {
			return nil, err
		}
	}
	return things, nil
}

func (s *toyService) ThingCount(_ context.Context, p *toy.ThingCountPayload) (int, error) {
	if p.Fail != nil && *p.Fail {
		return 0, toy.Teapot("short and stout")
	}
	return 3, nil
}

func (s *toyService) ThingBlob(context.Context, *toy.ThingBlobPayload) ([]byte, error) {
	return []byte{0x00, 0xff, 'h', 'i'}, nil
}
