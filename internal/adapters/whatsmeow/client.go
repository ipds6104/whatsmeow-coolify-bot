package whatsmeow

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/ipds6104/whatsmeow-coolify-bot/internal/domain"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/ports"
)

// ClientAdapter wraps *whatsmeow.Client to implement ports.WhatsAppClientPort.
type ClientAdapter struct {
	client *whatsmeow.Client
}

var _ ports.WhatsAppClientPort = (*ClientAdapter)(nil)

func NewClientAdapter(client *whatsmeow.Client) *ClientAdapter {
	return &ClientAdapter{client: client}
}

func (c *ClientAdapter) Connect() error {
	return c.client.Connect()
}

func (c *ClientAdapter) Disconnect() {
	c.client.Disconnect()
}

func (c *ClientAdapter) IsConnected() bool {
	return c.client.IsConnected()
}

func (c *ClientAdapter) IsLoggedIn() bool {
	return c.client.Store != nil && c.client.Store.ID != nil
}

func (c *ClientAdapter) GetDeviceJID() string {
	if c.client.Store != nil && c.client.Store.ID != nil {
		return c.client.Store.ID.String()
	}
	return ""
}

func (c *ClientAdapter) PairPhone(ctx context.Context, phone string, clientDisplayName string) (string, error) {
	if clientDisplayName == "" {
		clientDisplayName = "Chrome (Linux)"
	}
	return c.client.PairPhone(ctx, phone, true, whatsmeow.PairClientChrome, clientDisplayName)
}

func (c *ClientAdapter) SendTextMessage(ctx context.Context, to string, text string, replyID string) (string, error) {
	targetJID, err := c.parseJID(to)
	if err != nil {
		return "", err
	}

	msg := &waE2E.Message{
		Conversation: proto.String(text),
	}

	if replyID != "" {
		msg.ExtendedTextMessage = &waE2E.ExtendedTextMessage{
			Text: proto.String(text),
			ContextInfo: &waE2E.ContextInfo{
				StanzaID: proto.String(replyID),
			},
		}
		msg.Conversation = nil
	}

	resp, err := c.client.SendMessage(ctx, targetJID, msg)
	if err != nil {
		return "", err
	}

	return resp.ID, nil
}

func (c *ClientAdapter) SendMediaMessage(ctx context.Context, to string, media domain.MediaMessage) (string, error) {
	targetJID, err := c.parseJID(to)
	if err != nil {
		return "", err
	}

	var waMediaType whatsmeow.MediaType
	switch media.Type {
	case domain.MediaTypeImage:
		waMediaType = whatsmeow.MediaImage
	case domain.MediaTypeVideo:
		waMediaType = whatsmeow.MediaVideo
	case domain.MediaTypeAudio, domain.MediaTypeVoice:
		waMediaType = whatsmeow.MediaAudio
	default:
		waMediaType = whatsmeow.MediaDocument
	}

	uploaded, err := c.client.Upload(ctx, media.Data, waMediaType)
	if err != nil {
		return "", fmt.Errorf("failed to upload media: %w", err)
	}

	msg := &waE2E.Message{}
	switch media.Type {
	case domain.MediaTypeImage:
		msg.ImageMessage = &waE2E.ImageMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      proto.String(media.MimeType),
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uploaded.FileLength),
			Caption:       proto.String(media.Caption),
		}
	case domain.MediaTypeVideo:
		msg.VideoMessage = &waE2E.VideoMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      proto.String(media.MimeType),
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uploaded.FileLength),
			Caption:       proto.String(media.Caption),
		}
	case domain.MediaTypeAudio, domain.MediaTypeVoice:
		isPTT := media.Type == domain.MediaTypeVoice
		msg.AudioMessage = &waE2E.AudioMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      proto.String(media.MimeType),
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uploaded.FileLength),
			PTT:           proto.Bool(isPTT),
		}
	default:
		fileName := media.FileName
		if fileName == "" {
			fileName = "document.bin"
		}
		msg.DocumentMessage = &waE2E.DocumentMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      proto.String(media.MimeType),
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uploaded.FileLength),
			Title:         proto.String(fileName),
			FileName:      proto.String(fileName),
			Caption:       proto.String(media.Caption),
		}
	}

	resp, err := c.client.SendMessage(ctx, targetJID, msg)
	if err != nil {
		return "", err
	}

	return resp.ID, nil
}

func (c *ClientAdapter) DownloadMedia(ctx context.Context, info domain.MediaDownloadInfo) ([]byte, error) {
	encFileHash, err := hex.DecodeString(info.EncFileHash)
	if err != nil {
		return nil, fmt.Errorf("invalid enc_file_hash hex: %w", err)
	}

	fileSha256, err := hex.DecodeString(info.FileSha256)
	if err != nil {
		return nil, fmt.Errorf("invalid file_sha256 hex: %w", err)
	}

	mediaKey, err := hex.DecodeString(info.MediaKey)
	if err != nil {
		return nil, fmt.Errorf("invalid media_key hex: %w", err)
	}

	var waMediaType whatsmeow.MediaType
	switch info.MediaType {
	case string(domain.MediaTypeImage):
		waMediaType = whatsmeow.MediaImage
	case string(domain.MediaTypeVideo):
		waMediaType = whatsmeow.MediaVideo
	case string(domain.MediaTypeAudio), string(domain.MediaTypeVoice):
		waMediaType = whatsmeow.MediaAudio
	default:
		waMediaType = whatsmeow.MediaDocument
	}

	return c.client.DownloadMediaWithPath(
		ctx,
		info.DirectPath,
		encFileHash,
		fileSha256,
		mediaKey,
		waMediaType,
		"",
		false,
	)
}

func (c *ClientAdapter) GetGroupInfo(ctx context.Context, groupJID string) (domain.GroupInfo, error) {
	jid, err := types.ParseJID(groupJID)
	if err != nil {
		return domain.GroupInfo{}, fmt.Errorf("invalid group jid: %w", err)
	}

	rawInfo, err := c.client.GetGroupInfo(ctx, jid)
	if err != nil {
		return domain.GroupInfo{}, err
	}

	return mapGroupInfo(rawInfo), nil
}

func (c *ClientAdapter) GetJoinedGroups(ctx context.Context) ([]domain.GroupInfo, error) {
	rawGroups, err := c.client.GetJoinedGroups(ctx)
	if err != nil {
		return nil, err
	}

	groups := make([]domain.GroupInfo, 0, len(rawGroups))
	for _, raw := range rawGroups {
		groups = append(groups, mapGroupInfo(raw))
	}
	return groups, nil
}

func (c *ClientAdapter) AddEventHandler(handler func(evt interface{})) {
	c.client.AddEventHandler(handler)
}

func (c *ClientAdapter) parseJID(input string) (types.JID, error) {
	input = strings.TrimSpace(input)
	if strings.Contains(input, "@") {
		return types.ParseJID(input)
	}
	// Default to user chat if only phone digits provided
	phone := strings.TrimPrefix(input, "+")
	return types.NewJID(phone, types.DefaultUserServer), nil
}

func mapGroupInfo(raw *types.GroupInfo) domain.GroupInfo {
	if raw == nil {
		return domain.GroupInfo{}
	}

	participants := make([]domain.GroupParticipant, 0, len(raw.Participants))
	for _, p := range raw.Participants {
		participants = append(participants, domain.GroupParticipant{
			JID:          p.JID.String(),
			PhoneNumber:  p.JID.User,
			IsAdmin:      p.IsAdmin,
			IsSuperAdmin: p.IsSuperAdmin,
		})
	}

	return domain.GroupInfo{
		JID:          raw.JID.String(),
		Name:         raw.Name,
		OwnerJID:     raw.OwnerJID.String(),
		Topic:        raw.Topic,
		TopicSetBy:   raw.TopicSetBy.String(),
		TopicSetAt:   raw.TopicSetAt,
		CreatedAt:    raw.GroupCreated,
		IsAnnounce:   raw.IsAnnounce,
		IsLocked:     raw.IsLocked,
		MemberCount:  len(raw.Participants),
		Participants: participants,
	}
}
