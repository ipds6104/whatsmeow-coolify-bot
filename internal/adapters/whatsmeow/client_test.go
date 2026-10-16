package whatsmeow

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"

	"go.mau.fi/whatsmeow/types"
)

func TestEnsureJPEG_AlreadyJPEG(t *testing.T) {
	// Magic bytes for JPEG: 0xFF, 0xD8, 0xFF
	fakeJPEG := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F'}
	out, err := ensureJPEG(fakeJPEG)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(out, fakeJPEG) {
		t.Fatalf("expected untouched byte slice for already JPEG data")
	}
}

func TestEnsureJPEG_ConvertsPNGToJPEG(t *testing.T) {
	// Create a 2x2 test PNG in memory
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	img.Set(1, 1, color.RGBA{R: 0, G: 255, B: 0, A: 255})

	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatalf("failed to encode test PNG: %v", err)
	}

	pngData := pngBuf.Bytes()
	// Confirm it's PNG
	if len(pngData) < 8 || string(pngData[1:4]) != "PNG" {
		t.Fatalf("test data is not PNG")
	}

	out, err := ensureJPEG(pngData)
	if err != nil {
		t.Fatalf("ensureJPEG failed on PNG input: %v", err)
	}

	// Confirm output starts with JPEG SOI magic bytes (0xFF, 0xD8, 0xFF)
	if len(out) < 3 || out[0] != 0xFF || out[1] != 0xD8 || out[2] != 0xFF {
		t.Fatalf("output is not valid JPEG magic bytes: %X", out[:3])
	}
}

func TestEnsureJPEG_InvalidDataPassesThrough(t *testing.T) {
	invalidData := []byte("not an image at all")
	out, err := ensureJPEG(invalidData)
	if err != nil {
		t.Fatalf("unexpected error on invalid data fallback: %v", err)
	}
	if !bytes.Equal(out, invalidData) {
		t.Fatalf("expected pass-through on non-image data")
	}
}

type mockLIDStore struct {
	pnToLID map[string]string
	lidToPN map[string]string
}

func (m *mockLIDStore) GetLIDForPN(_ context.Context, pn types.JID) (types.JID, error) {
	if lid, ok := m.pnToLID[pn.User]; ok {
		return types.NewJID(lid, types.HiddenUserServer), nil
	}
	return types.JID{}, nil
}

func (m *mockLIDStore) GetPNForLID(_ context.Context, lid types.JID) (types.JID, error) {
	if pn, ok := m.lidToPN[lid.User]; ok {
		return types.NewJID(pn, types.DefaultUserServer), nil
	}
	return types.JID{}, nil
}

func TestResolveMentions_GroupWithLID_ReplacesPhoneWithLID(t *testing.T) {
	groupJID := types.NewJID("120363377989532476", types.GroupServer)
	groupInfo := &types.GroupInfo{
		JID: groupJID,
		Participants: []types.GroupParticipant{
			{
				JID:         types.NewJID("87097809592405", types.HiddenUserServer),
				LID:         types.NewJID("87097809592405", types.HiddenUserServer),
				PhoneNumber: types.NewJID("6289625345646", types.DefaultUserServer),
			},
			{
				JID:         types.NewJID("211733364924422", types.HiddenUserServer),
				LID:         types.NewJID("211733364924422", types.HiddenUserServer),
				PhoneNumber: types.NewJID("6285772372655", types.DefaultUserServer),
			},
		},
	}

	text := "Halo @6289625345646 dan @6285772372655 yaa"
	newText, mentions := resolveMentions(context.Background(), text, groupJID, nil, groupInfo, nil)

	expectedText := "Halo @87097809592405 dan @211733364924422 yaa"
	if newText != expectedText {
		t.Errorf("expected text %q, got %q", expectedText, newText)
	}

	expectedMentions := []string{"87097809592405@lid", "211733364924422@lid"}
	if len(mentions) != 2 || mentions[0] != expectedMentions[0] || mentions[1] != expectedMentions[1] {
		t.Errorf("expected mentions %v, got %v", expectedMentions, mentions)
	}
}

func TestResolveMentions_GroupWithDirectLID_KeepsLID(t *testing.T) {
	groupJID := types.NewJID("120363377989532476", types.GroupServer)
	groupInfo := &types.GroupInfo{
		JID: groupJID,
		Participants: []types.GroupParticipant{
			{
				JID: types.NewJID("87097809592405", types.HiddenUserServer),
				LID: types.NewJID("87097809592405", types.HiddenUserServer),
			},
		},
	}

	text := "Halo @87097809592405!"
	newText, mentions := resolveMentions(context.Background(), text, groupJID, nil, groupInfo, nil)

	if newText != text {
		t.Errorf("expected untouched text %q, got %q", text, newText)
	}
	if len(mentions) != 1 || mentions[0] != "87097809592405@lid" {
		t.Errorf("expected mention [87097809592405@lid], got %v", mentions)
	}
}

func TestResolveMentions_1on1_NoFakeLID(t *testing.T) {
	userJID := types.NewJID("6289625345646", types.DefaultUserServer)
	text := "Halo @6289625345646!"
	newText, mentions := resolveMentions(context.Background(), text, userJID, nil, nil, nil)

	if newText != text {
		t.Errorf("expected untouched text %q, got %q", text, newText)
	}
	if len(mentions) != 1 || mentions[0] != "6289625345646@s.whatsapp.net" {
		t.Errorf("expected only [6289625345646@s.whatsapp.net], got %v", mentions)
	}
}
