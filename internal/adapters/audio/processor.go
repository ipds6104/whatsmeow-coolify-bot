package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"

	"github.com/ipds6104/whatsmeow-coolify-bot/internal/ports"
)

// Processor implements ports.AudioProcessorPort following SOLID principles.
// It is responsible exclusively for parsing audio metadata and synthesizing WhatsApp-compliant waveforms.
type Processor struct{}

var _ ports.AudioProcessorPort = (*Processor)(nil)

func NewProcessor() *Processor {
	return &Processor{}
}

// ProcessAudio extracts the duration in seconds and computes a 64-byte normalized waveform.
func (p *Processor) ProcessAudio(ctx context.Context, data []byte, mimeType string) (ports.AudioMetadata, error) {
	duration := p.detectDuration(data, mimeType)
	if duration == 0 {
		duration = 1 // Strict invariant: WhatsApp mobile requires duration >= 1 second
	}

	waveform := p.generateWaveform(data)

	return ports.AudioMetadata{
		DurationSeconds: duration,
		Waveform:        waveform,
	}, nil
}

// detectDuration attempts to accurately read the granule position from the last Ogg page.
func (p *Processor) detectDuration(data []byte, mimeType string) uint32 {
	if len(data) < 28 {
		return 1
	}

	// 1. OGG Container Parsing (OggS magic signature)
	// Check if this is an OGG stream by scanning backwards for the last OggS page header.
	pattern := []byte("OggS")
	lastIdx := bytes.LastIndex(data, pattern)
	if lastIdx >= 0 && lastIdx+14 <= len(data) {
		// In Ogg, bytes 6..14 (8 bytes, little endian) store the granule position.
		granulePos := binary.LittleEndian.Uint64(data[lastIdx+6 : lastIdx+14])
		// 0xFFFFFFFFFFFFFFFF (-1) indicates no valid packet ends on this page.
		if granulePos > 0 && granulePos != 0xFFFFFFFFFFFFFFFF {
			// Opus clock rate is standard 48,000 Hz
			seconds := uint32(granulePos / 48000)
			if seconds > 0 {
				return seconds
			}
		}
	}

	// 2. Fallback heuristic based on bitrates for common voice streams (~32kbps Opus)
	// 32,000 bits per second = 4,000 bytes per second
	approxSeconds := uint32(len(data) / 4000)
	if approxSeconds > 0 {
		return approxSeconds
	}

	return 1
}

// generateWaveform creates a 64-byte slice with values from 5 to 100 representing amplitude peaks.
func (p *Processor) generateWaveform(data []byte) []byte {
	const waveformBars = 64
	waveform := make([]byte, waveformBars)

	if len(data) == 0 {
		for i := range waveform {
			waveform[i] = 10
		}
		return waveform
	}

	chunkSize := len(data) / waveformBars
	if chunkSize == 0 {
		chunkSize = 1
	}

	// Sample raw bytes to approximate energy levels across the recording
	for i := 0; i < waveformBars; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if start >= len(data) {
			waveform[i] = 15
			continue
		}
		if end > len(data) {
			end = len(data)
		}

		var sum float64
		count := 0
		for j := start; j < end; j += 2 {
			val := float64(data[j]) - 128.0
			sum += val * val
			count++
		}

		var rms float64
		if count > 0 {
			rms = math.Sqrt(sum / float64(count))
		}

		// Normalize to WhatsApp bar range: 5 (quiet) to 100 (peak)
		normalized := uint8(math.Min(100, math.Max(5, (rms/128.0)*100.0*1.5)))
		waveform[i] = normalized
	}

	return waveform
}
