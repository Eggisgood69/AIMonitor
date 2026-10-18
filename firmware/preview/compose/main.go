// 把 firmware/preview 產生的 240×240 PPM 合成為 README 用的圖片（圓形遮罩、深灰底）
package main

import (
	"bufio"
	"fmt"
	"image"
	"image/color"
	"image/color/palette"
	"image/draw"
	"image/gif"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
)

var bg = color.RGBA{30, 30, 34, 255}

func readPPM(p string) *image.RGBA {
	f, err := os.Open(p)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	r := bufio.NewReader(f)
	var w, h, mx int
	fmt.Fscanf(r, "P6 %d %d %d\n", &w, &h, &mx)
	im := image.NewRGBA(image.Rect(0, 0, w, h))
	buf := make([]byte, w*h*3)
	if _, err := io.ReadFull(r, buf); err != nil {
		panic(err)
	}
	for i := 0; i < w*h; i++ {
		im.Pix[i*4], im.Pix[i*4+1], im.Pix[i*4+2], im.Pix[i*4+3] = buf[i*3], buf[i*3+1], buf[i*3+2], 255
	}
	return im
}

// 縮放（雙線性）＋圓形遮罩（邊緣抗鋸齒），畫到 dst 的 (ox, oy)
func put(dst *image.RGBA, src *image.RGBA, ox, oy, size int) {
	s := float64(src.Bounds().Dx()) / float64(size)
	R := float64(size) / 2
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := float64(x)+0.5-R, float64(y)+0.5-R
			a := math.Max(0, math.Min(1, R-math.Hypot(dx, dy)+0.5))
			if a == 0 {
				continue
			}
			fx, fy := (float64(x)+0.5)*s-0.5, (float64(y)+0.5)*s-0.5
			x0, y0 := int(math.Floor(fx)), int(math.Floor(fy))
			tx, ty := fx-float64(x0), fy-float64(y0)
			var c [3]float64
			for k := 0; k < 4; k++ {
				xx, yy := x0+k%2, y0+k/2
				if xx < 0 {
					xx = 0
				}
				if yy < 0 {
					yy = 0
				}
				if xx > 239 {
					xx = 239
				}
				if yy > 239 {
					yy = 239
				}
				wx, wy := 1-tx, 1-ty
				if k%2 == 1 {
					wx = tx
				}
				if k/2 == 1 {
					wy = ty
				}
				wgt := wx * wy
				o := src.PixOffset(xx, yy)
				for j := 0; j < 3; j++ {
					c[j] += wgt * float64(src.Pix[o+j])
				}
			}
			o := dst.PixOffset(ox+x, oy+y)
			for j := 0; j < 3; j++ {
				dst.Pix[o+j] = uint8(a*c[j] + (1-a)*float64(dst.Pix[o+j]) + 0.5)
			}
		}
	}
}

func strip(in string, names []string, out string) {
	const size, gap = 360, 20
	W := gap + len(names)*(size+gap)
	im := image.NewRGBA(image.Rect(0, 0, W, size+2*gap))
	draw.Draw(im, im.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)
	for i, n := range names {
		put(im, readPPM(filepath.Join(in, n+".ppm")), gap+i*(size+gap), gap, size)
	}
	w, _ := os.Create(out)
	png.Encode(w, im)
	w.Close()
}

func main() {
	// 用法：go run main.go <PPM 目錄> <輸出目錄>
	in, outDir := os.Args[1], os.Args[2]
	strip(in, []string{"review", "working", "done", "error", "idle", "nolink"}, filepath.Join(outDir, "preview.png"))
	strip(in, []string{"idle", "page1", "page2", "page3", "saver"}, filepath.Join(outDir, "preview-pages.png"))
	strip(in, []string{"saver"}, filepath.Join(outDir, "preview-saver.png"))
	// 動畫 GIF：320×320，每格 80ms
	fs, _ := filepath.Glob(filepath.Join(in, "anim_*.ppm"))
	sort.Strings(fs)
	g := &gif.GIF{}
	for _, f := range fs {
		im := image.NewRGBA(image.Rect(0, 0, 320, 320))
		draw.Draw(im, im.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)
		put(im, readPPM(f), 0, 0, 320)
		p := image.NewPaletted(im.Bounds(), palette.Plan9)
		draw.Draw(p, p.Bounds(), im, image.Point{}, draw.Src)
		g.Image = append(g.Image, p)
		g.Delay = append(g.Delay, 8)
	}
	w, _ := os.Create(filepath.Join(outDir, "preview.gif"))
	gif.EncodeAll(w, g)
	w.Close()
	fmt.Println("frames", len(fs))
}
