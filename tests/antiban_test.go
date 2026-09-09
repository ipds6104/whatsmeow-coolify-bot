package tests

import (
	"context"
	"testing"
	"time"

	"github.com/ipds6104/whatsmeow-coolify-bot/internal/adapters/antiban"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/domain"
)

func TestAntiBanGuard_Presets(t *testing.T) {
	guardStrict := antiban.NewGuard("strict")
	stats := guardStrict.GetWarmupStats()
	if stats["preset"] != "strict" {
		t.Errorf("expected preset strict, got %v", stats["preset"])
	}

	guardRelaxed := antiban.NewGuard("relaxed")
	statsRelaxed := guardRelaxed.GetWarmupStats()
	if statsRelaxed["preset"] != "relaxed" {
		t.Errorf("expected preset relaxed, got %v", statsRelaxed["preset"])
	}
}

func TestAntiBanGuard_PermitAndRecord(t *testing.T) {
	guard := antiban.NewGuard("relaxed")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	target := "6281234567890@s.whatsapp.net"

	// First send should pass immediately (no prior sends)
	err := guard.WaitSendPermit(ctx, target)
	if err != nil {
		t.Fatalf("first permit should succeed: %v", err)
	}

	guard.RecordMessageSent(target)
	stats := guard.GetWarmupStats()
	if stats["daily_count"] != 1 {
		t.Errorf("expected daily_count 1, got %v", stats["daily_count"])
	}
}

func TestAntiBanGuard_DailyCap(t *testing.T) {
	guard := antiban.NewGuard("strict")
	target := "6281234567890@s.whatsapp.net"

	// Artificially simulate reaching daily cap
	for i := 0; i < 80; i++ {
		guard.RecordMessageSent(target)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := guard.WaitSendPermit(ctx, target)
	if err == nil {
		t.Fatalf("expected error due to daily cap, got nil")
	}

	if !isError(err, domain.ErrWarmupCapReached) {
		t.Errorf("expected ErrWarmupCapReached, got: %v", err)
	}
}

func isError(err, target error) bool {
	if err == nil {
		return target == nil
	}
	for {
		if err == target {
			return true
		}
		type unwrapper interface {
			Unwrap() error
		}
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
}
