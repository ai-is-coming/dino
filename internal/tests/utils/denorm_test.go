package utils_test

import (
	"testing"

	"github.com/ai-is-coming/dino/internal/utils"
)

func TestDenormalizeBbox_BasicSquare(t *testing.T) {
	x1, y1, x2, y2 := utils.DenormalizeBbox("0", "0", "999", "999", 1000, 1000, 1000)
	if x1 != 0 || y1 != 0 || x2 != 999 || y2 != 999 {
		t.Fatalf("got (%d,%d,%d,%d), want (0,0,999,999)", x1, y1, x2, y2)
	}
}

func TestDenormalizeBbox_RectFull(t *testing.T) {
	// width=1920, height=1080; 999 maps to floor(999/1000 * dim)
	x1, y1, x2, y2 := utils.DenormalizeBbox("0", "0", "999", "999", 1920, 1080, 1000)
	if x1 != 0 || y1 != 0 || x2 != 1918 || y2 != 1078 {
		t.Fatalf("got (%d,%d,%d,%d), want (0,0,1918,1078)", x1, y1, x2, y2)
	}
}

func TestDenormalizeBbox_OrderSwap(t *testing.T) {
	// inputs where x1>x2 and y1>y2 should be normalized to x1<=x2, y1<=y2
	x1, y1, x2, y2 := utils.DenormalizeBbox("800", "300", "200", "100", 1000, 500, 1000)
	// expected: x: 800->800, 200->200 => (200,800); y: 300->150, 100->50 => (50,150)
	if x1 != 200 || y1 != 50 || x2 != 800 || y2 != 150 {
		t.Fatalf("got (%d,%d,%d,%d), want (200,50,800,150)", x1, y1, x2, y2)
	}
}

func TestDenormalizeBbox_Clamp(t *testing.T) {
	// values out of 0..999 range should clamp to [0..w-1]/[0..h-1]
	x1, y1, x2, y2 := utils.DenormalizeBbox("1200", "-10", "2000", "1500", 640, 480, 1000)
	if x1 != 639 || y1 != 0 || x2 != 639 || y2 != 479 {
		t.Fatalf("got (%d,%d,%d,%d), want (639,0,639,479)", x1, y1, x2, y2)
	}
}

func TestDenormalizeBbox_MidPoints(t *testing.T) {
	x1, y1, x2, y2 := utils.DenormalizeBbox("500", "500", "750", "750", 1920, 1080, 1000)
	if x1 != 960 || y1 != 540 || x2 != 1440 || y2 != 810 {
		t.Fatalf("got (%d,%d,%d,%d), want (960,540,1440,810)", x1, y1, x2, y2)
	}
}
