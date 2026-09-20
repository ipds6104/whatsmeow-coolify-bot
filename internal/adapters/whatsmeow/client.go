package whatsmeow

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"github.com/ipds6104/whatsmeow-coolify-bot/internal/domain"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/ports"
)

// ClientAdapter wraps *whatsmeow.Client to implement ports.WhatsAppClientPort.
type ClientAdapter struct {
	client    *whatsmeow.Client
	container *sqlstore.Container
	waLogger  waLog.Logger
	handlers  []func(evt interface{})
	mu        sync.RWMutex
}

var _ ports.WhatsAppClientPort = (*ClientAdapter)(nil)

func NewClientAdapter(client *whatsmeow.Client, container *sqlstore.Container, waLogger waLog.Logger) *ClientAdapter {
	return &ClientAdapter{
		client:    client,
		container: container,
		waLogger:  waLogger,
		handlers:  make([]func(evt interface{}), 0),
	}
}

func (c *ClientAdapter) getClient() *whatsmeow.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.client
}

func (c *ClientAdapter) AddEventHandler(handler func(evt interface{})) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers = append(c.handlers, handler)
	if c.client != nil {
		c.client.AddEventHandler(handler)
	}
}

func (c *ClientAdapter) ResetDevice(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.container == nil {
		return errors.New("cannot reset device: sqlstore container is nil")
	}

	log.Println("[WHATSMEOW-SELF-HEAL] Provisioning a fresh in-memory device store...")
	if c.client != nil {
		c.client.Disconnect()
	}

	newDevice := c.container.NewDevice()
	newClient := whatsmeow.NewClient(newDevice, c.waLogger)
	for _, handler := range c.handlers {
		newClient.AddEventHandler(handler)
	}
	c.client = newClient
	log.Println("[WHATSMEOW-SELF-HEAL] Fresh device store and whatsmeow client re-initialized.")
	return nil
}

func (c *ClientAdapter) ensureValidDevice(ctx context.Context) error {
	client := c.getClient()
	if client != nil && client.Store != nil && !client.Store.Deleted {
		return nil
	}

	log.Println("[WHATSMEOW-SELF-HEAL] Device is marked deleted or missing. Recreating fresh device...")
	return c.ResetDevice(ctx)
}

func (c *ClientAdapter) Connect() error {
	if err := c.ensureValidDevice(context.Background()); err != nil {
		return err
	}

	client := c.getClient()
	err := client.Connect()
	if errors.Is(err, store.ErrDeviceDeleted) {
		log.Println("[WHATSMEOW-SELF-HEAL] Connect failed with ErrDeviceDeleted. Auto-recovering...")
		if resetErr := c.ResetDevice(context.Background()); resetErr != nil {
			return resetErr
		}
		return c.getClient().Connect()
	}
	return err
}

func (c *ClientAdapter) Disconnect() {
	client := c.getClient()
	if client != nil {
		client.Disconnect()
	}
}

func (c *ClientAdapter) IsConnected() bool {
	client := c.getClient()
	return client != nil && client.IsConnected()
}

func (c *ClientAdapter) IsLoggedIn() bool {
	client := c.getClient()
	return client != nil && client.Store != nil && client.Store.ID != nil
}

func (c *ClientAdapter) GetDeviceJID() string {
	client := c.getClient()
	if client != nil && client.Store != nil && client.Store.ID != nil {
		return client.Store.ID.String()
	}
	return ""
}

func (c *ClientAdapter) GetDeviceLID() string {
	client := c.getClient()
	if client != nil && client.Store != nil && !client.Store.LID.IsEmpty() {
		return client.Store.LID.ToNonAD().String()
	}
	return ""
}

func (c *ClientAdapter) PairPhone(ctx context.Context, phone string, clientDisplayName string) (string, error) {
	if err := c.ensureValidDevice(ctx); err != nil {
		return "", err
	}

	if clientDisplayName == "" {
		clientDisplayName = "Chrome (Linux)"
	}

	client := c.getClient()
	code, err := client.PairPhone(ctx, phone, true, whatsmeow.PairClientChrome, clientDisplayName)
	if errors.Is(err, store.ErrDeviceDeleted) {
		log.Println("[WHATSMEOW-SELF-HEAL] PairPhone encountered ErrDeviceDeleted. Auto-recovering fresh device...")
		if resetErr := c.ResetDevice(ctx); resetErr != nil {
			return "", resetErr
		}
		freshClient := c.getClient()
		if !freshClient.IsConnected() {
			if connErr := freshClient.Connect(); connErr != nil {
				return "", fmt.Errorf("failed to connect after auto-recover: %w", connErr)
			}
			time.Sleep(1500 * time.Millisecond)
		}
		return freshClient.PairPhone(ctx, phone, true, whatsmeow.PairClientChrome, clientDisplayName)
	}
	return code, err
}

var mentionRegex = regexp.MustCompile(`@(\d{8,16})`)

func (c *ClientAdapter) SendTextMessage(ctx context.Context, to string, text string, replyID string, mentions []string) (string, error) {
	targetJID, err := c.parseJID(to)
	if err != nil {
		return "", err
	}

	// Prepare mentioned JIDs
	mentionedJIDs := make([]string, 0, len(mentions)+4)
	existing := make(map[string]bool)

	for _, m := range mentions {
		m = strings.TrimSpace(m)
		if m != "" && !existing[m] {
			mentionedJIDs = append(mentionedJIDs, m)
			existing[m] = true
		}
	}

	// Auto-detect @<digits> in text (e.g. @6289625345646 or @87097809592405)
	matches := mentionRegex.FindAllStringSubmatch(text, -1)
	for _, match := range matches {
		if len(match) > 1 {
			num := match[1]
			userJID := num + "@s.whatsapp.net"
			if !existing[userJID] {
				mentionedJIDs = append(mentionedJIDs, userJID)
				existing[userJID] = true
			}
			lidJID := num + "@lid"
			if !existing[lidJID] {
				mentionedJIDs = append(mentionedJIDs, lidJID)
				existing[lidJID] = true
			}
		}
	}

	msg := &waE2E.Message{}

	if replyID != "" || len(mentionedJIDs) > 0 {
		ctxInfo := &waE2E.ContextInfo{}
		if replyID != "" {
			ctxInfo.StanzaID = proto.String(replyID)
		}
		if len(mentionedJIDs) > 0 {
			ctxInfo.MentionedJID = mentionedJIDs
		}

		msg.ExtendedTextMessage = &waE2E.ExtendedTextMessage{
			Text:        proto.String(text),
			ContextInfo: ctxInfo,
		}
	} else {
		msg.Conversation = proto.String(text)
	}

	resp, err := c.getClient().SendMessage(ctx, targetJID, msg)
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

	uploaded, err := c.getClient().Upload(ctx, media.Data, waMediaType)
	if err != nil {
		return "", fmt.Errorf("failed to upload media: %w", err)
	}

	if media.MimeType == "" {
		media.MimeType = http.DetectContentType(media.Data)
	}

	var ctxInfo *waE2E.ContextInfo
	if media.ReplyToID != "" {
		ctxInfo = &waE2E.ContextInfo{
			StanzaID: proto.String(media.ReplyToID),
		}
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
			ContextInfo:   ctxInfo,
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
			ContextInfo:   ctxInfo,
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
			ContextInfo:   ctxInfo,
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
			ContextInfo:   ctxInfo,
		}
	}

	resp, err := c.getClient().SendMessage(ctx, targetJID, msg)
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

	return c.getClient().DownloadMediaWithPath(
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

	rawInfo, err := c.getClient().GetGroupInfo(ctx, jid)
	if err != nil {
		return domain.GroupInfo{}, err
	}

	return mapGroupInfo(rawInfo), nil
}

func (c *ClientAdapter) GetJoinedGroups(ctx context.Context) ([]domain.GroupInfo, error) {
	rawGroups, err := c.getClient().GetJoinedGroups(ctx)
	if err != nil {
		return nil, err
	}

	groups := make([]domain.GroupInfo, 0, len(rawGroups))
	for _, raw := range rawGroups {
		groups = append(groups, mapGroupInfo(raw))
	}
	return groups, nil
}


func (c *ClientAdapter) SendChatPresence(ctx context.Context, to string, state string) error {
	recipientJID, err := c.parseJID(to)
	if err != nil {
		return fmt.Errorf("invalid recipient JID %q: %w", to, err)
	}

	var chatPresence types.ChatPresence
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "composing", "typing":
		chatPresence = types.ChatPresenceComposing
	case "paused", "stop", "stopped":
		chatPresence = types.ChatPresencePaused
	default:
		chatPresence = types.ChatPresenceComposing
	}

	return c.getClient().SendChatPresence(ctx, recipientJID, chatPresence, types.ChatPresenceMediaText)
}

func (c *ClientAdapter) GetProfilePicture(ctx context.Context, jidStr string, preview bool) (*domain.ProfilePictureResult, error) {
	var target types.JID
	jidStr = strings.TrimSpace(jidStr)
	client := c.getClient()
	if jidStr == "" || strings.EqualFold(jidStr, "me") || strings.EqualFold(jidStr, "self") {
		if client.Store == nil || client.Store.ID == nil {
			return nil, fmt.Errorf("client is not logged in")
		}
		target = client.Store.ID.ToNonAD()
	} else {
		var err error
		target, err = c.parseJID(jidStr)
		if err != nil {
			return nil, fmt.Errorf("invalid target JID %q: %w", jidStr, err)
		}
	}

	params := &whatsmeow.GetProfilePictureParams{
		Preview: preview,
	}
	info, err := client.GetProfilePictureInfo(ctx, target, params)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, fmt.Errorf("profile picture not found or unchanged")
	}

	return &domain.ProfilePictureResult{
		JID:  target.String(),
		URL:  info.URL,
		ID:   info.ID,
		Type: info.Type,
	}, nil
}

func (c *ClientAdapter) SetProfilePicture(ctx context.Context, jidStr string, avatar []byte) (string, error) {
	var target types.JID
	jidStr = strings.TrimSpace(jidStr)
	client := c.getClient()
	if jidStr == "" || strings.EqualFold(jidStr, "me") || strings.EqualFold(jidStr, "self") {
		if client.Store == nil || client.Store.ID == nil {
			return "", fmt.Errorf("client is not logged in")
		}
		target = client.Store.ID.ToNonAD()
	} else {
		var err error
		target, err = c.parseJID(jidStr)
		if err != nil {
			return "", fmt.Errorf("invalid target JID %q: %w", jidStr, err)
		}
	}

	return client.SetGroupPhoto(ctx, target, avatar)
}

func (c *ClientAdapter) SetStatusMessage(ctx context.Context, status string) error {
	return c.getClient().SetStatusMessage(ctx, types.SetStatusInput{
		Text: &status,
	})
}

func (c *ClientAdapter) SendStatusBroadcast(ctx context.Context, status domain.StatusBroadcastMessage) (string, error) {
	if status.Type == domain.MediaTypeImage || status.Type == domain.MediaTypeVideo {
		caption := status.Caption
		if caption == "" {
			caption = status.Text
		}
		mediaMsg := domain.MediaMessage{
			Recipient: types.StatusBroadcastJID.String(),
			Type:      status.Type,
			Data:      status.Data,
			Caption:   caption,
		}
		return c.SendMediaMessage(ctx, types.StatusBroadcastJID.String(), mediaMsg)
	}

	extText := &waE2E.ExtendedTextMessage{
		Text: proto.String(status.Text),
	}
	if status.BackgroundColor != 0 {
		extText.BackgroundArgb = proto.Uint32(status.BackgroundColor)
	}
	if status.Font != 0 {
		extText.Font = waE2E.ExtendedTextMessage_FontType(status.Font).Enum()
	}

	waMsg := &waE2E.Message{
		ExtendedTextMessage: extText,
	}

	resp, err := c.getClient().SendMessage(ctx, types.StatusBroadcastJID, waMsg)
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

func (c *ClientAdapter) RevokeMessage(ctx context.Context, chatJID string, messageID string) error {
	var chat types.JID
	chatJID = strings.TrimSpace(chatJID)
	if chatJID == "" || strings.EqualFold(chatJID, "status") || strings.EqualFold(chatJID, "status@broadcast") {
		chat = types.StatusBroadcastJID
	} else {
		var err error
		chat, err = c.parseJID(chatJID)
		if err != nil {
			return fmt.Errorf("invalid chat JID %q: %w", chatJID, err)
		}
	}

	client := c.getClient()
	revokeMsg := client.BuildRevoke(chat, types.EmptyJID, types.MessageID(messageID))
	_, err := client.SendMessage(ctx, chat, revokeMsg)
	return err
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
