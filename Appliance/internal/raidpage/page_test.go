package raidpage

import (
	"bytes"
	"testing"
)

func TestRenderRaidReceiptUsesNativeLetterGeometry(t *testing.T) {
	gray, report, err := Render(Receipt{Channel: "Raid Channel", Viewers: 47})
	if err != nil {
		t.Fatal(err)
	}
	if report.WidthDots != 1664 || report.Rows != 2233 || len(gray) != 1664*2233 {
		t.Fatalf("report=%#v len=%d", report, len(gray))
	}
	if report.Channel != "RAID CHANNEL" || report.Viewers != 47 {
		t.Fatalf("report=%#v", report)
	}
	if bytes.Count(gray, []byte{0}) < 100 {
		t.Fatal("receipt has insufficient ink")
	}
}

func TestRaidReceiptRejectsInvalidAndUnsupportedInput(t *testing.T) {
	for _, r := range []Receipt{{Channel: "", Viewers: 10}, {Channel: "Raid", Viewers: 0}, {Channel: "Raid💚", Viewers: 10}} {
		if _, _, err := Render(r); err == nil {
			t.Fatalf("accepted %#v", r)
		}
	}
}

func TestRaidReceiptKeepsLongChannelNameInsideBounds(t *testing.T) {
	gray, report, err := Render(Receipt{Channel: "ABCDEFGHIJKLMNOPQRSTUVWXYZ", Viewers: 999})
	if err != nil {
		t.Fatal(err)
	}
	if report.Channel == "" || len(gray) != 1664*2233 {
		t.Fatalf("report=%#v", report)
	}
}
