// Package attachment stores application-owned image copies independently of source files.
package attachment

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
)

const MaxBytes = 10 << 20
const MaxPixels = 24_000_000
const MaxImages = 8

type Image struct {
	Path string `json:"path,omitempty"`
	URL  string `json:"url,omitempty"`
	Name string `json:"name,omitempty"`
}

func Read(ref Image) ([]byte, error) {
	if ref.Path != "" {
		f, err := os.Open(ref.Path)
		if err != nil {
			return nil, fmt.Errorf("图片文件不可用：%w", err)
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
		if err != nil {
			return nil, err
		}
		return validate(data)
	}
	if strings.HasPrefix(ref.URL, "data:image/") {
		header, value, ok := strings.Cut(ref.URL, ",")
		if !ok || !strings.HasSuffix(header, ";base64") || len(value) > base64.StdEncoding.EncodedLen(MaxBytes) {
			return nil, fmt.Errorf("图片数据无效或超过 10 MiB")
		}
		data, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return nil, fmt.Errorf("图片数据无法解码")
		}
		return validate(data)
	}
	return nil, fmt.Errorf("图片不可用，请重新添加本地图片")
}

func validate(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data) > MaxBytes {
		return nil, fmt.Errorf("每张图片需小于 10 MiB")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("不支持的图片格式或文件已损坏")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return nil, fmt.Errorf("图片尺寸超过 2400 万像素")
	}
	return data, nil
}

func Import(dir string, ref Image) (Image, error) {
	data, err := Read(ref)
	if err != nil {
		return Image{}, err
	}
	decoded, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Image{}, fmt.Errorf("图片文件已损坏")
	}
	// Codex receives a supported still-image format for GIF/BMP sources.
	if format == "gif" || format == "bmp" {
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, decoded); err != nil {
			return Image{}, err
		}
		data, format = encoded.Bytes(), "png"
		if len(data) > MaxBytes {
			return Image{}, fmt.Errorf("转换后的图片超过 10 MiB")
		}
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return Image{}, err
	}
	hash := sha256.Sum256(data)
	target := filepath.Join(dir, fmt.Sprintf("%x.%s", hash, format))
	if _, err := os.Stat(target); os.IsNotExist(err) {
		f, err := os.CreateTemp(dir, ".image-*")
		if err != nil {
			return Image{}, err
		}
		tmp := f.Name()
		defer os.Remove(tmp)
		_, writeErr := f.Write(data)
		closeErr := f.Close()
		if writeErr != nil {
			return Image{}, writeErr
		}
		if closeErr != nil {
			return Image{}, closeErr
		}
		if err := os.Rename(tmp, target); err != nil {
			return Image{}, err
		}
	} else if err != nil {
		return Image{}, err
	}
	name := ref.Name
	if name == "" {
		name = filepath.Base(ref.Path)
	}
	if name == "" || name == "." {
		name = "截图.png"
	}
	return Image{Path: target, Name: name}, nil
}
