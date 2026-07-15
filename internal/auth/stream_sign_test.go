package auth

import (
	"strings"
	"testing"
)

func TestAccountStreamSignRoundTrip(t *testing.T) {
	sign, err := SignAccountStreamURL(42, "/media/video.mkv", 1)
	if err != nil {
		t.Fatal(err)
	}
	accountID, identity, err := VerifyAccountStreamSign(sign)
	if err != nil {
		t.Fatal(err)
	}
	if accountID != 42 || identity != "/media/video.mkv" {
		t.Fatalf("accountID=%d identity=%q", accountID, identity)
	}
}

func TestAccountStreamSignRejectsTampering(t *testing.T) {
	sign, err := SignAccountStreamURL(42, "/media/video.mkv", 0)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(sign, ":")
	parts[3] = "dGFtcGVyZWQ"
	if _, _, err := VerifyAccountStreamSign(strings.Join(parts, ":")); err == nil {
		t.Fatal("tampered identity was accepted")
	}
}

func TestAccountStreamSignRejectsInvalidInput(t *testing.T) {
	if _, err := SignAccountStreamURL(0, "/media/video.mkv", 0); err == nil {
		t.Fatal("zero account ID was accepted")
	}
	if _, err := SignAccountStreamURL(1, "/media/video.mkv", -1); err == nil {
		t.Fatal("negative expiry was accepted")
	}
	if _, _, err := VerifyAccountStreamSign("invalid"); err == nil {
		t.Fatal("invalid signature format was accepted")
	}
}
