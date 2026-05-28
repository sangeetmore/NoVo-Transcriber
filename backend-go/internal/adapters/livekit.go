package adapters

import (
	"context"
	"fmt"
	"time"

	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/sangeetmore/novo-transcriber/backend-go/internal/config"
)

// LiveKitClient manages room lifecycle and token generation for the capture client.
type LiveKitClient struct {
	cfg        *config.Config
	roomName   string
	roomClient *lksdk.RoomServiceClient
}

// NewLiveKitClient initializes the server-side LiveKit client.
func NewLiveKitClient(cfg *config.Config) *LiveKitClient {
	client := lksdk.NewRoomServiceClient(cfg.LiveKitURL, cfg.LiveKitAPIKey, cfg.LiveKitAPISecret)
	return &LiveKitClient{
		cfg:        cfg,
		roomClient: client,
	}
}

// RoomName returns the currently active room name.
func (lk *LiveKitClient) RoomName() string {
	return lk.roomName
}

// CreateRoom asks the LiveKit server to create a room.
func (lk *LiveKitClient) CreateRoom(ctx context.Context, roomName string) error {
	req := &livekit.CreateRoomRequest{
		Name:         roomName,
		EmptyTimeout: 300,
	}
	room, err := lk.roomClient.CreateRoom(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to create LiveKit room: %w", err)
	}
	lk.roomName = room.Name
	return nil
}

// DeleteRoom asks the LiveKit server to delete a room.
func (lk *LiveKitClient) DeleteRoom(ctx context.Context, roomName string) error {
	req := &livekit.DeleteRoomRequest{
		Room: roomName,
	}
	_, err := lk.roomClient.DeleteRoom(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to delete LiveKit room: %w", err)
	}
	return nil
}

// GenerateParticipantToken creates an access token for a client to join the room.
func (lk *LiveKitClient) GenerateParticipantToken(identity, roomName string, ttl time.Duration) (string, error) {
	at := auth.NewAccessToken(lk.cfg.LiveKitAPIKey, lk.cfg.LiveKitAPISecret)
	grant := &auth.VideoGrant{
		RoomJoin:     true,
		Room:         roomName,
		CanPublish:   func(b bool) *bool { return &b }(true),
		CanSubscribe: func(b bool) *bool { return &b }(true),
	}
	at.AddGrant(grant).
		SetIdentity(identity).
		SetValidFor(ttl)

	return at.ToJWT()
}

// LiveKitEventStream represents a channel of normalized events from tracks.
type LiveKitEventStream struct {
	Events chan map[string]any
	done   chan struct{}
}

// NewEventStream creates an event stream to pass into the orchestrator.
func (lk *LiveKitClient) NewEventStream() *LiveKitEventStream {
	return &LiveKitEventStream{
		Events: make(chan map[string]any, 256),
		done:   make(chan struct{}),
	}
}

// Close closes the event stream.
func (es *LiveKitEventStream) Close() {
	close(es.done)
	close(es.Events)
}
