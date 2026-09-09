package antiban

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ipds6104/whatsmeow-coolify-bot/internal/domain"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/ports"
)

type PresetConfig struct {
	MinDelay       time.Duration
	MaxDelay       time.Duration
	MaxBurst       int
	DailyWarmupCap int
}

var Presets = map[string]PresetConfig{
	"strict": {
		MinDelay:       4 * time.Second,
		MaxDelay:       8 * time.Second,
		MaxBurst:       5,
		DailyWarmupCap: 80,
	},
	"moderate": {
		MinDelay:       2 * time.Second,
		MaxDelay:       5 * time.Second,
		MaxBurst:       15,
		DailyWarmupCap: 300,
	},
	"relaxed": {
		MinDelay:       1 * time.Second,
		MaxDelay:       3 * time.Second,
		MaxBurst:       30,
		DailyWarmupCap: 1000,
	},
}

// Guard implements ports.AntiBanGuardPort to protect the WhatsApp account from bans.
type Guard struct {
	mu            sync.Mutex
	cfg           PresetConfig
	presetName    string
	lastSentTimes map[string]time.Time // recipient -> last sent time
	lastGlobal    time.Time
	dailyCount    int
	currentDay    int
}

// Ensure Guard implements ports.AntiBanGuardPort
var _ ports.AntiBanGuardPort = (*Guard)(nil)

func NewGuard(preset string) *Guard {
	cfg, ok := Presets[preset]
	if !ok {
		cfg = Presets["moderate"]
		preset = "moderate"
	}

	return &Guard{
		cfg:           cfg,
		presetName:    preset,
		lastSentTimes: make(map[string]time.Time),
		currentDay:    time.Now().YearDay(),
	}
}

// WaitSendPermit throttles message dispatch with human-like jitter delays and enforces daily warmup caps.
func (g *Guard) WaitSendPermit(ctx context.Context, targetJID string) error {
	g.mu.Lock()

	now := time.Now()
	// Reset daily counter on new day
	if now.YearDay() != g.currentDay {
		g.currentDay = now.YearDay()
		g.dailyCount = 0
	}

	if g.dailyCount >= g.cfg.DailyWarmupCap {
		g.mu.Unlock()
		return fmt.Errorf("%w: current limit is %d messages/day for preset '%s'",
			domain.ErrWarmupCapReached, g.cfg.DailyWarmupCap, g.presetName)
	}

	// Calculate humanized jitter delay
	jitter := g.randomJitter(g.cfg.MinDelay, g.cfg.MaxDelay)

	// Ensure min interval since global last sent
	elapsedGlobal := now.Sub(g.lastGlobal)
	var waitDuration time.Duration
	if elapsedGlobal < jitter {
		waitDuration = jitter - elapsedGlobal
	}

	// Ensure per-recipient interval
	if lastTarget, exists := g.lastSentTimes[targetJID]; exists {
		elapsedTarget := now.Sub(lastTarget)
		if elapsedTarget < (g.cfg.MinDelay * 2) {
			targetWait := (g.cfg.MinDelay * 2) - elapsedTarget
			if targetWait > waitDuration {
				waitDuration = targetWait
			}
		}
	}

	g.mu.Unlock()

	if waitDuration > 0 {
		select {
		case <-time.After(waitDuration):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// RecordMessageSent updates timestamps and increments the daily counter after successful transmission.
func (g *Guard) RecordMessageSent(targetJID string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now()
	g.lastGlobal = now
	g.lastSentTimes[targetJID] = now
	g.dailyCount++
}

// GetWarmupStats returns diagnostic statistics about current message rates.
func (g *Guard) GetWarmupStats() map[string]interface{} {
	g.mu.Lock()
	defer g.mu.Unlock()

	return map[string]interface{}{
		"preset":           g.presetName,
		"daily_count":      g.dailyCount,
		"daily_cap":        g.cfg.DailyWarmupCap,
		"min_delay_ms":     g.cfg.MinDelay.Milliseconds(),
		"max_delay_ms":     g.cfg.MaxDelay.Milliseconds(),
		"last_global_sent": g.lastGlobal,
	}
}

func (g *Guard) randomJitter(min, max time.Duration) time.Duration {
	delta := max - min
	if delta <= 0 {
		return min
	}
	nBig, err := rand.Int(rand.Reader, big.NewInt(delta.Nanoseconds()))
	if err != nil {
		return min
	}
	return min + time.Duration(nBig.Int64())
}
