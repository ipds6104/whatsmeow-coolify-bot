package audio

import (
	"context"
	"testing"
)

func TestProcessor_ProcessAudio_EmptyData(t *testing.T) {
	p := NewProcessor()
	meta, err := p.ProcessAudio(context.Background(), []byte{}, "audio/ogg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.DurationSeconds < 1 {
		t.Errorf("expected DurationSeconds >= 1, got %d", meta.DurationSeconds)
	}
	if len(meta.Waveform) != 64 {
		t.Errorf("expected 64 waveform bytes, got %d", len(meta.Waveform))
	}
}

func TestProcessor_ProcessAudio_Fallback(t *testing.T) {
	p := NewProcessor()
	dummyAudio := make([]byte, 12000) // ~3 seconds at 4KB/s
	meta, err := p.ProcessAudio(context.Background(), dummyAudio, "audio/ogg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.DurationSeconds != 3 {
		t.Errorf("expected 3 seconds, got %d", meta.DurationSeconds)
	}
	if len(meta.Waveform) != 64 {
		t.Errorf("expected 64 waveform bars, got %d", len(meta.Waveform))
	}
}
