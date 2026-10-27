package attachment

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/bmp"
)

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestImageNormalizesGIFAndBMPForSending(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 10, 8))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	for _, format := range []string{"gif", "bmp"} {
		t.Run(format, func(t *testing.T) {
			var encoded bytes.Buffer
			var err error
			if format == "gif" {
				err = gif.Encode(&encoded, img, nil)
			} else {
				err = bmp.Encode(&encoded, img)
			}
			if err != nil {
				t.Fatal(err)
			}
			ref, err := Import(t.TempDir(), Image{URL: "data:image/" + format + ";base64," + base64.StdEncoding.EncodeToString(encoded.Bytes()), Name: "source." + format})
			if err != nil {
				t.Fatal(err)
			}
			raw, err := Read(ref)
			if err != nil {
				t.Fatal(err)
			}
			cfg, decodedFormat, err := image.DecodeConfig(bytes.NewReader(raw))
			if err != nil || decodedFormat != "png" || cfg.Width != 10 || cfg.Height != 8 || filepath.Ext(ref.Path) != ".png" || ref.Name != "source."+format {
				t.Fatal("source was not normalized to a named still PNG", ref, cfg, decodedFormat, err)
			}
		})
	}
}

func TestImageCopiesDeduplicatesAndSurvivesSourceRemoval(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "原图.png")
	raw := testPNG(t, 20, 10)
	if err := os.WriteFile(source, raw, 0600); err != nil {
		t.Fatal(err)
	}
	owned, err := Import(filepath.Join(dir, "owned"), Image{Path: source})
	if err != nil {
		t.Fatal(err)
	}
	pasted, err := Import(filepath.Join(dir, "owned"), Image{URL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw), Name: "截图.png"})
	if err != nil || owned.Path != pasted.Path {
		t.Fatal("same content was not deduplicated", err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	got, err := Read(owned)
	if err != nil || !bytes.Equal(raw, got) || owned.Name != "原图.png" {
		t.Fatal("owned copy lost", err)
	}
	if info, _ := os.Stat(owned.Path); info.Mode().Perm() != 0600 {
		t.Fatal("image copy is not private")
	}
}

func TestImageRejectsInvalidOversizedAndMissingInputs(t *testing.T) {
	for _, raw := range [][]byte{nil, []byte("not an image"), make([]byte, MaxBytes+1), testPNG(t, 1, 1)[:33]} {
		_, err := Import(t.TempDir(), Image{URL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)})
		if err == nil {
			t.Fatal("invalid image accepted")
		}
	}
	for _, ref := range []Image{{Path: filepath.Join(t.TempDir(), "missing.png")}, {URL: "https://example.com/picture.png"}, {URL: "data:image/png;base64,not-base64"}} {
		if _, err := Read(ref); err == nil {
			t.Fatal("unavailable image accepted")
		}
	}
}

func TestThumbnailLimitsDecodedConversationImage(t *testing.T) {
	img, err := Thumbnail(testPNG(t, 1280, 720))
	if err != nil || img.Bounds().Dx() != 640 || img.Bounds().Dy() != 360 {
		t.Fatal("thumbnail not bounded", err)
	}
	src := image.NewRGBA(image.Rect(0, 0, 3, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 3; x++ {
			src.Set(x, y, color.RGBA{R: uint8(x*50 + y*10), A: 255})
		}
	}
	for o := 2; o <= 8; o++ {
		rotated := orientedImage{Image: src, orientation: o}
		if o >= 5 && (rotated.Bounds().Dx() != 2 || rotated.Bounds().Dy() != 3) {
			t.Fatal("rotated dimensions wrong")
		}
		seen := map[color.Color]bool{}
		for y := 0; y < rotated.Bounds().Dy(); y++ {
			for x := 0; x < rotated.Bounds().Dx(); x++ {
				seen[rotated.At(x, y)] = true
			}
		}
		if len(seen) != 6 {
			t.Fatal("orientation lost image pixels", o)
		}
	}
}
