// 把 firmware/preview 產生的 PPM 合成為 README 用的圖片：
// 透明背景（GitHub 淺色 / 深色主題都適用）、螢幕外加一圈機身邊框與柔和陰影
package main

import (
	"bufio"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
)

// 用法：go run main.go <PPM 目錄（含 466/）> <輸出目錄（docs/images）>
// 預覽圖用 1.75 吋 AMOLED 的畫面（解析度高、有 Clawd）
func main() {
	in, outDir := filepath.Join(os.Args[1], "466"), os.Args[2]
	grid(in, [][]string{{"review", "working", "done"}, {"error", "idle", "nolink"}}, 300, filepath.Join(outDir, "states.png"))
	grid(in, [][]string{{"idle", "page1", "page2", "page3"}}, 300, filepath.Join(outDir, "pages.png"))
	n := animGIF(in, 320, filepath.Join(outDir, "demo.gif"))
	fmt.Println("frames", n)
}

// 一個螢幕（含邊框、陰影）占的格子大小
func cellSize(d int) int { return int(float64(d) * 1.2) }

func grid(in string, rows [][]string, d int, out string) {
	c := cellSize(d)
	cols := 0
	for _, r := range rows {
		cols = max(cols, len(r))
	}
	im := image.NewNRGBA(image.Rect(0, 0, cols*c, len(rows)*c))
	for j, r := range rows {
		off := (cols - len(r)) * c / 2 // 這一列較短時置中
		for i, n := range r {
			device(im, readPPM(filepath.Join(in, n+".ppm")), off+i*c+c/2, j*c+c/2, d, true)
		}
	}
	savePNG(out, im)
}

// 在 (cx, cy) 畫一台螢幕：陰影 → 機身邊框 → 畫面（圓形遮罩，邊緣抗鋸齒）
func device(dst *image.NRGBA, scr *image.RGBA, cx, cy, d int, shadow bool) {
	R := float64(d) / 2 // 畫面半徑
	B := R * 1.055      // 機身外緣
	S := float64(scr.Bounds().Dx()) / float64(d)
	ext := int(B*1.12) + 2
	for y := cy - ext; y < cy+ext; y++ {
		for x := cx - ext; x < cx+ext; x++ {
			if !(image.Point{x, y}.In(dst.Rect)) {
				continue
			}
			fx, fy := float64(x)+0.5-float64(cx), float64(y)+0.5-float64(cy)
			r := math.Hypot(fx, fy)
			// 陰影：往下偏一點，邊緣慢慢淡掉
			if shadow {
				rs := math.Hypot(fx, fy-R*0.04)
				if a := 0.32 * (1 - smooth(B*0.92, B*1.12, rs)); a > 0 {
					over(dst, x, y, 0, 0, 0, a)
				}
			}
			// 機身：上亮下暗的深灰，外緣一條細亮線
			if a := clamp(B - r + 0.5); a > 0 {
				t := (fy/B + 1) / 2
				g := 64 - 36*t
				over(dst, x, y, g, g, g+3, a)
				if hl := clamp(1.2 - math.Abs(r-(B-1.2))); hl > 0 {
					over(dst, x, y, 120, 120, 126, hl*0.55*(1-t))
				}
			}
			// 畫面：雙線性縮放
			if a := clamp(R - r + 0.5); a > 0 {
				rr, gg, bb := sample(scr, (float64(x-cx)+R)*S, (float64(y-cy)+R)*S)
				over(dst, x, y, rr, gg, bb, a)
			}
		}
	}
}

func sample(src *image.RGBA, fx, fy float64) (r, g, b float64) {
	w := src.Bounds().Dx()
	fx, fy = fx-0.5, fy-0.5
	x0, y0 := int(math.Floor(fx)), int(math.Floor(fy))
	tx, ty := fx-float64(x0), fy-float64(y0)
	var c [3]float64
	for k := 0; k < 4; k++ {
		xx, yy := min(max(x0+k%2, 0), w-1), min(max(y0+k/2, 0), w-1)
		wx, wy := 1-tx, 1-ty
		if k%2 == 1 {
			wx = tx
		}
		if k/2 == 1 {
			wy = ty
		}
		o := src.PixOffset(xx, yy)
		for j := 0; j < 3; j++ {
			c[j] += wx * wy * float64(src.Pix[o+j])
		}
	}
	return c[0], c[1], c[2]
}

// 把顏色 (r, g, b)（0-255）以不透明度 a 疊到 dst 上（dst 是非預乘 alpha）
func over(dst *image.NRGBA, x, y int, r, g, b, a float64) {
	o := dst.PixOffset(x, y)
	p := dst.Pix[o : o+4]
	da := float64(p[3]) / 255
	oa := a + da*(1-a)
	if oa <= 0 {
		return
	}
	mix := func(s float64, d uint8) uint8 { return uint8((s*a+float64(d)*da*(1-a))/oa + 0.5) }
	p[0], p[1], p[2], p[3] = mix(r, p[0]), mix(g, p[1]), mix(b, p[2]), uint8(oa*255+0.5)
}

func clamp(v float64) float64 { return math.Max(0, math.Min(1, v)) }

func smooth(e0, e1, x float64) float64 {
	t := clamp((x - e0) / (e1 - e0))
	return t * t * (3 - 2*t)
}

// 動畫 GIF：GIF 只有全透明 / 不透明，所以不畫陰影，邊框外緣以 50% 為界
func animGIF(in string, d int, out string) int {
	fs, _ := filepath.Glob(filepath.Join(in, "anim_*.ppm"))
	sort.Strings(fs)
	c := int(float64(d) * 1.08)
	var frames []*image.NRGBA
	for _, f := range fs {
		im := image.NewNRGBA(image.Rect(0, 0, c, c))
		device(im, readPPM(f), c/2, c/2, d, false)
		frames = append(frames, im)
	}
	pal := gifPalette(frames)
	g := &gif.GIF{}
	for _, im := range frames {
		p := image.NewPaletted(im.Bounds(), pal)
		cache := map[uint32]uint8{}
		for i := range p.Pix {
			o := i * 4
			if im.Pix[o+3] < 128 {
				continue // 0 = 透明
			}
			k := uint32(im.Pix[o])<<16 | uint32(im.Pix[o+1])<<8 | uint32(im.Pix[o+2])
			idx, ok := cache[k]
			if !ok {
				idx = uint8(pal[1:].Index(color.NRGBA{im.Pix[o], im.Pix[o+1], im.Pix[o+2], 255}) + 1)
				cache[k] = idx
			}
			p.Pix[i] = idx
		}
		g.Image = append(g.Image, p)
		g.Delay = append(g.Delay, 8)
		g.Disposal = append(g.Disposal, gif.DisposalBackground)
	}
	w, err := os.Create(out)
	if err != nil {
		panic(err)
	}
	defer w.Close()
	if err := gif.EncodeAll(w, g); err != nil {
		panic(err)
	}
	return len(fs)
}

// 從所有畫面挑最常出現的 255 種顏色（每色 5 位元分桶），第 0 個留給透明
func gifPalette(frames []*image.NRGBA) color.Palette {
	type bucket struct{ n, r, g, b int }
	hist := map[uint16]*bucket{}
	for _, im := range frames {
		for o := 0; o < len(im.Pix); o += 4 {
			if im.Pix[o+3] < 128 {
				continue
			}
			r, g, b := int(im.Pix[o]), int(im.Pix[o+1]), int(im.Pix[o+2])
			k := uint16(r>>3)<<10 | uint16(g>>3)<<5 | uint16(b>>3)
			bk := hist[k]
			if bk == nil {
				bk = &bucket{}
				hist[k] = bk
			}
			bk.n++
			bk.r += r
			bk.g += g
			bk.b += b
		}
	}
	list := make([]*bucket, 0, len(hist))
	for _, bk := range hist {
		list = append(list, bk)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].n > list[j].n })
	pal := color.Palette{color.Transparent}
	for _, bk := range list {
		if len(pal) == 256 {
			break
		}
		pal = append(pal, color.NRGBA{uint8(bk.r / bk.n), uint8(bk.g / bk.n), uint8(bk.b / bk.n), 255})
	}
	return pal
}

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

func savePNG(p string, im image.Image) {
	w, err := os.Create(p)
	if err != nil {
		panic(err)
	}
	defer w.Close()
	if err := png.Encode(w, im); err != nil {
		panic(err)
	}
}
