// Command genicon renders the easyTTY app icon (build/icon.png) and the same
// picture for the UI (frontend/dist/logo.png).
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
)

const (
	size = 256
	ss   = 4 // supersampling per axis
)

func inPolygon(x, y float64, pts [][2]float64) bool {
	in := false
	for i, j := 0, len(pts)-1; i < len(pts); j, i = i, i+1 {
		xi, yi, xj, yj := pts[i][0], pts[i][1], pts[j][0], pts[j][1]
		if (yi > y) != (yj > y) && x < (xj-xi)*(y-yi)/(yj-yi)+xi {
			in = !in
		}
	}
	return in
}

func hexagon(cx, cy, r float64) [][2]float64 {
	pts := make([][2]float64, 6)
	for i := range pts {
		a := math.Pi/6 + float64(i)*math.Pi/3
		pts[i] = [2]float64{cx + r*math.Cos(a), cy + r*math.Sin(a)}
	}
	return pts
}

func main() {
	outer := hexagon(128, 128, 124)
	inner := hexagon(128, 128, 104)
	chevron := [][2]float64{{78, 84}, {100, 84}, {146, 128}, {100, 172}, {78, 172}, {124, 128}}
	cursor := [][2]float64{{150, 156}, {196, 156}, {196, 174}, {150, 174}}

	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var r, g, b, a float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					x := float64(px) + (float64(sx)+0.5)/ss
					y := float64(py) + (float64(sy)+0.5)/ss
					switch {
					case inPolygon(x, y, chevron) || inPolygon(x, y, cursor):
						r, g, b, a = r+235, g+245, b+255, a+1
					case inPolygon(x, y, inner):
						t := y / size // vertical gradient
						r, g, b, a = r+18+20*t, g+24+20*t, b+36+24*t, a+1
					case inPolygon(x, y, outer):
						t := (x + y) / (2 * size)
						r, g, b, a = r+79-40*t, g+179-40*t, b+255-20*t, a+1
					}
				}
			}
			n := float64(ss * ss)
			if a == 0 {
				continue
			}
			img.SetNRGBA(px, py, color.NRGBA{R: uint8(r / a), G: uint8(g / a), B: uint8(b / a), A: uint8(255 * a / n)})
		}
	}
	for _, out := range []string{filepath.Join("build", "icon.png"), filepath.Join("frontend", "dist", "logo.png")} {
		if err := writePNG(out, img); err != nil {
			log.Fatal(err)
		}
	}
}

func writePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
