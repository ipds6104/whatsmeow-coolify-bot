package whatsmeow

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
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

type cachedGroup struct {
	info      *types.GroupInfo
	fetchedAt time.Time
}

// ClientAdapter wraps *whatsmeow.Client to implement ports.WhatsAppClientPort.
type ClientAdapter struct {
	client       *whatsmeow.Client
	container    *sqlstore.Container
	waLogger     waLog.Logger
	handlers     []func(evt interface{})
	mu           sync.RWMutex
	groupCache   map[types.JID]cachedGroup
	groupCacheMu sync.RWMutex
}

var _ ports.WhatsAppClientPort = (*ClientAdapter)(nil)

func NewClientAdapter(client *whatsmeow.Client, container *sqlstore.Container, waLogger waLog.Logger) *ClientAdapter {
	return &ClientAdapter{
		client:     client,
		container:  container,
		waLogger:   waLogger,
		handlers:   make([]func(evt interface{}), 0),
		groupCache: make(map[types.JID]cachedGroup),
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

func (c *ClientAdapter) getGroupInfoCached(ctx context.Context, groupJID types.JID) *types.GroupInfo {
	c.groupCacheMu.RLock()
	cached, ok := c.groupCache[groupJID]
	c.groupCacheMu.RUnlock()

	if ok && time.Since(cached.fetchedAt) < 10*time.Minute && cached.info != nil {
		return cached.info
	}

	client := c.getClient()
	if client == nil || !client.IsConnected() {
		if ok {
			return cached.info
		}
		return nil
	}

	info, err := client.GetGroupInfo(ctx, groupJID)
	if err != nil {
		if ok {
			return cached.info
		}
		return nil
	}

	c.groupCacheMu.Lock()
	if c.groupCache == nil {
		c.groupCache = make(map[types.JID]cachedGroup)
	}
	c.groupCache[groupJID] = cachedGroup{
		info:      info,
		fetchedAt: time.Now(),
	}
	c.groupCacheMu.Unlock()

	return info
}

var mentionRegex = regexp.MustCompile(`@(\d{8,16})`)

type lidStoreReader interface {
	GetLIDForPN(ctx context.Context, pn types.JID) (types.JID, error)
	GetPNForLID(ctx context.Context, lid types.JID) (types.JID, error)
}

func resolveMentions(
	ctx context.Context,
	text string,
	targetJID types.JID,
	explicitMentions []string,
	groupInfo *types.GroupInfo,
	lidStore lidStoreReader,
) (string, []string) {
	mentionedJIDs := make([]string, 0, len(explicitMentions)+4)
	existing := make(map[string]bool)

	// 1. Process explicit mentions supplied by caller
	for _, m := range explicitMentions {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if !strings.Contains(m, "@") {
			if len(m) >= 14 && (strings.HasPrefix(m, "1") || strings.HasPrefix(m, "2") || strings.HasPrefix(m, "8")) && !strings.HasPrefix(m, "62") {
				m = m + "@" + types.HiddenUserServer
			} else {
				m = m + "@" + types.DefaultUserServer
			}
		}
		if !existing[m] {
			mentionedJIDs = append(mentionedJIDs, m)
			existing[m] = true
		}
	}

	// 2. Process text mentions
	if targetJID.Server == types.GroupServer {
		matches := mentionRegex.FindAllStringSubmatch(text, -1)
		for _, match := range matches {
			if len(match) <= 1 {
				continue
			}
			rawNum := match[1] // e.g. "6289625345646" or "87097809592405"

			var resolvedJID types.JID
			var resolved bool

			// A. Match against group participants if available
			if groupInfo != nil {
				for _, p := range groupInfo.Participants {
					if (p.PhoneNumber.User != "" && p.PhoneNumber.User == rawNum) ||
						(p.LID.User != "" && p.LID.User == rawNum) ||
						(p.JID.User == rawNum) {
						if !p.LID.IsEmpty() {
							resolvedJID = p.LID
						} else {
							resolvedJID = p.JID
						}
						resolved = true
						break
					}
				}
			}

			// B. Fallback to SQL store LID mapping if available
			if !resolved && lidStore != nil {
				pnJID := types.NewJID(rawNum, types.DefaultUserServer)
				if lid, err := lidStore.GetLIDForPN(ctx, pnJID); err == nil && !lid.IsEmpty() {
					resolvedJID = lid
					resolved = true
				} else {
					lidTest := types.NewJID(rawNum, types.HiddenUserServer)
					if pn, err := lidStore.GetPNForLID(ctx, lidTest); err == nil && !pn.IsEmpty() {
						resolvedJID = lidTest
						resolved = true
					}
				}
			}

			if resolved && !resolvedJID.IsEmpty() {
				// In an LID group, WhatsApp requires the token in text to match the LID number
				if resolvedJID.Server == types.HiddenUserServer && rawNum != resolvedJID.User {
					text = strings.ReplaceAll(text, "@"+rawNum, "@"+resolvedJID.User)
				}
				jidStr := resolvedJID.String()
				if !existing[jidStr] {
					mentionedJIDs = append(mentionedJIDs, jidStr)
					existing[jidStr] = true
				}
			} else {
				// Fallback if not resolved in group:
				// Determine if rawNum looks like an LID or standard PN. NEVER append both!
				var fallbackJID string
				if len(rawNum) >= 14 && (strings.HasPrefix(rawNum, "1") || strings.HasPrefix(rawNum, "2") || strings.HasPrefix(rawNum, "8")) && !strings.HasPrefix(rawNum, "62") {
					fallbackJID = rawNum + "@" + types.HiddenUserServer
				} else {
					fallbackJID = rawNum + "@" + types.DefaultUserServer
				}
				if !existing[fallbackJID] {
					mentionedJIDs = append(mentionedJIDs, fallbackJID)
					existing[fallbackJID] = true
				}
			}
		}
	} else {
		// 1-on-1 chats: standard user mention
		matches := mentionRegex.FindAllStringSubmatch(text, -1)
		for _, match := range matches {
			if len(match) > 1 {
				num := match[1]
				userJID := num + "@" + types.DefaultUserServer
				if !existing[userJID] {
					mentionedJIDs = append(mentionedJIDs, userJID)
					existing[userJID] = true
				}
			}
		}
	}

	return text, mentionedJIDs
}

func (c *ClientAdapter) SendTextMessage(ctx context.Context, to string, text string, replyID string, mentions []string) (string, error) {
	targetJID, err := c.parseJID(to)
	if err != nil {
		return "", err
	}

	var groupInfo *types.GroupInfo
	if targetJID.Server == types.GroupServer {
		groupInfo = c.getGroupInfoCached(ctx, targetJID)
	}

	client := c.getClient()
	var lidStore lidStoreReader
	if client != nil && client.Store != nil && client.Store.LIDs != nil {
		lidStore = client.Store.LIDs
	}

	processedText, mentionedJIDs := resolveMentions(ctx, text, targetJID, mentions, groupInfo, lidStore)

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
			Text:        proto.String(processedText),
			ContextInfo: ctxInfo,
		}
	} else {
		msg.Conversation = proto.String(processedText)
	}

	resp, err := client.SendMessage(ctx, targetJID, msg)
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
		isPTT := media.Type == domain.MediaTypeVoice || media.PTT
		mimetype := media.MimeType
		if isPTT || strings.Contains(strings.ToLower(mimetype), "ogg") || strings.Contains(strings.ToLower(mimetype), "opus") {
			mimetype = "audio/ogg; codecs=opus"
		}

		seconds := media.Seconds
		if seconds == 0 {
			seconds = 1
		}

		audioMsg := &waE2E.AudioMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      proto.String(mimetype),
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uploaded.FileLength),
			Seconds:       proto.Uint32(seconds),
			PTT:           proto.Bool(isPTT),
			ContextInfo:   ctxInfo,
		}
		if len(media.Waveform) > 0 {
			audioMsg.Waveform = media.Waveform
		}
		msg.AudioMessage = audioMsg
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

func ensureJPEG(data []byte) ([]byte, error) {
	if len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return data, nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		// If decoding fails, return original data so whatsmeow can validate
		return data, nil
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		return nil, fmt.Errorf("failed to encode avatar to JPEG: %w", err)
	}
	return buf.Bytes(), nil
}

func (c *ClientAdapter) SetProfilePicture(ctx context.Context, jidStr string, avatar []byte) (string, error) {
	var target types.JID
	jidStr = strings.TrimSpace(jidStr)
	client := c.getClient()
	if jidStr == "" || strings.EqualFold(jidStr, "me") || strings.EqualFold(jidStr, "self") {
		if client.Store == nil || client.Store.ID == nil {
			return "", fmt.Errorf("client is not logged in")
		}
		// In whatsmeow, SetGroupPhoto with types.EmptyJID updates the user's own profile photo
		target = types.EmptyJID
	} else {
		var err error
		target, err = c.parseJID(jidStr)
		if err != nil {
			return "", fmt.Errorf("invalid target JID %q: %w", jidStr, err)
		}
		// If caller passed the bot's own individual JID, normalize to EmptyJID for setting own profile photo
		if client.Store != nil && client.Store.ID != nil && target == client.Store.ID.ToNonAD() {
			target = types.EmptyJID
		}
	}

	if len(avatar) > 0 {
		var err error
		avatar, err = ensureJPEG(avatar)
		if err != nil {
			return "", err
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
		phone := p.PhoneNumber.User
		if phone == "" && p.JID.Server != types.HiddenUserServer {
			phone = p.JID.User
		}
		participants = append(participants, domain.GroupParticipant{
			JID:          p.JID.String(),
			PhoneNumber:  phone,
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
