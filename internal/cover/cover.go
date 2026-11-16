// Package cover 负责封面缩略图：把 cache/covers 的原图缩成 120px 宽的小图。
package cover

import (
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"

	"golang.org/x/image/draw"
)

// MakeThumb 生成缩略图；源图解码失败时返回错误（调用方忽略即可）。
func MakeThumb(src, dst string, width int) error {
	if width <= 0 {
		width = 120
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	img, _, err := image.Decode(in)
	_ = in.Close()
	if err != nil {
		return err
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return fmt.Errorf("封面尺寸非法")
	}
	w := width
	if w > b.Dx() {
		w = b.Dx()
	}
	h := int(float64(b.Dy()) * float64(w) / float64(b.Dx()))
	if h < 1 {
		h = 1
	}
	dstImg := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dstImg, dstImg.Bounds(), img, b, draw.Over, nil)

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := jpeg.Encode(f, dstImg, &jpeg.Options{Quality: 82}); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
