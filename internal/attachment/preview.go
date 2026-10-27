package attachment

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"

	"golang.org/x/image/draw"
)

// Thumbnail keeps decoded conversation images small; originals remain intact.
func Thumbnail(data []byte) (image.Image, error) {
	src, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if format == "jpeg" {
		if orientation := jpegOrientation(data); orientation >= 2 && orientation <= 8 {
			src = orientedImage{Image: src, orientation: orientation}
		}
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if max(w, h) <= 640 {
		return src, nil
	}
	ratio := float64(640) / float64(max(w, h))
	dst := image.NewRGBA(image.Rect(0, 0, max(1, int(float64(w)*ratio)), max(1, int(float64(h)*ratio))))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	return dst, nil
}

type orientedImage struct {
	image.Image
	orientation int
}

func (o orientedImage) Bounds() image.Rectangle {
	b := o.Image.Bounds()
	if o.orientation >= 5 {
		return image.Rect(0, 0, b.Dy(), b.Dx())
	}
	return image.Rect(0, 0, b.Dx(), b.Dy())
}
func (o orientedImage) At(x, y int) color.Color {
	b := o.Image.Bounds()
	w, h := b.Dx(), b.Dy()
	switch o.orientation {
	case 2:
		x = w - 1 - x
	case 3:
		x, y = w-1-x, h-1-y
	case 4:
		y = h - 1 - y
	case 5:
		x, y = y, x
	case 6:
		x, y = y, h-1-x
	case 7:
		x, y = w-1-y, h-1-x
	case 8:
		x, y = w-1-y, x
	}
	return o.Image.At(b.Min.X+x, b.Min.Y+y)
}

func jpegOrientation(data []byte) int {
	if len(data) < 2 || data[0] != 0xff || data[1] != 0xd8 {
		return 1
	}
	for p := 2; p+4 <= len(data); {
		if data[p] != 0xff {
			return 1
		}
		marker := data[p+1]
		if marker == 0xda || marker == 0xd9 {
			return 1
		}
		n := int(binary.BigEndian.Uint16(data[p+2 : p+4]))
		if n < 2 || p+2+n > len(data) {
			return 1
		}
		section := data[p+4 : p+2+n]
		if marker == 0xe1 && len(section) >= 14 && string(section[:6]) == "Exif\x00\x00" {
			tiff := section[6:]
			var order binary.ByteOrder
			switch string(tiff[:2]) {
			case "II":
				order = binary.LittleEndian
			case "MM":
				order = binary.BigEndian
			default:
				return 1
			}
			offset := uint64(order.Uint32(tiff[4:8]))
			if offset+2 > uint64(len(tiff)) {
				return 1
			}
			count := int(order.Uint16(tiff[offset : offset+2]))
			for i := 0; i < count; i++ {
				start := offset + 2 + uint64(i)*12
				if start+12 > uint64(len(tiff)) {
					return 1
				}
				entry := tiff[start : start+12]
				if order.Uint16(entry[:2]) == 0x112 && order.Uint16(entry[2:4]) == 3 && order.Uint32(entry[4:8]) == 1 {
					return int(order.Uint16(entry[8:10]))
				}
			}
		}
		p += n + 2
	}
	return 1
}
