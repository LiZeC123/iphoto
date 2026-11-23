package similar

import (
	"image"
	"image/color"
	"testing"
)

// horizontalGradient 生成「左亮右暗」的水平渐变（dHash 每行相邻比较均为 1）。
func horizontalGradient(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint8(255 - x*255/(w-1))
			img.SetNRGBA(x, y, color.NRGBA{R: v, G: v, B: v, A: 255})
		}
	}
	return img
}

// verticalGradient 生成「每行颜色相同」的纵向渐变（行内相邻比较均为 0）。
func verticalGradient(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		v := uint8(255 - y*255/(h-1))
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: v, G: v, B: v, A: 255})
		}
	}
	return img
}

func TestDHashKnownPatterns(t *testing.T) {
	horizontal := DHash(horizontalGradient(90, 80))
	vertical := DHash(verticalGradient(90, 80))
	if horizontal == vertical {
		t.Fatal("横向与纵向渐变的指纹不应相同")
	}
	// 横向渐变每一行的相邻比较都是「左 < 右」→ 全 1；纵向渐变行内相等 → 全 0。
	if horizontal != ^uint64(0) {
		t.Fatalf("向左变暗的渐变指纹 = %016x, want %016x", horizontal, ^uint64(0))
	}
	if vertical != 0 {
		t.Fatalf("纵向渐变指纹 = %016x, want 0", vertical)
	}
	if d := Hamming(horizontal, vertical); d != 64 {
		t.Fatalf("距离 = %d, want 64", d)
	}
}

func TestDHashStable(t *testing.T) {
	img := horizontalGradient(120, 90)
	first := DHash(img)
	second := DHash(img)
	if first != second {
		t.Fatalf("同一图片两次指纹不同: %016x / %016x", first, second)
	}
}

func TestDHashSmallDifference(t *testing.T) {
	// 9×8 原图会走 Resize 的同尺寸 Clone 分支，改动左列只影响每行第一位的比较。
	base := DHash(horizontalGradient(9, 8))
	img := horizontalGradient(9, 8)
	for y := 0; y < 8; y++ {
		img.SetNRGBA(0, y, color.NRGBA{A: 255})
	}
	changed := DHash(img)
	distance := Hamming(base, changed)
	if distance == 0 {
		t.Fatal("修改左上角后指纹应发生变化")
	}
	if distance > 8 {
		t.Fatalf("局部修改的距离 = %d，预期较小（<= 8）", distance)
	}
}

func TestSignatureFormatRoundTrip(t *testing.T) {
	cases := []uint64{0, 1, 0x0f0f0f0f0f0f0f0f, ^uint64(0)}
	for _, want := range cases {
		raw := FormatSignature(want)
		if len(raw) != 16 {
			t.Fatalf("FormatSignature(%d) = %q, want 16 位十六进制", want, raw)
		}
		got, err := ParseSignature(raw)
		if err != nil {
			t.Fatalf("ParseSignature(%q): %v", raw, err)
		}
		if got != want {
			t.Fatalf("round trip = %d, want %d", got, want)
		}
	}
	for _, bad := range []string{"", "xyz", "0123456789abcdef0"} {
		if _, err := ParseSignature(bad); err == nil {
			t.Errorf("ParseSignature(%q) 应该报错", bad)
		}
	}
}
