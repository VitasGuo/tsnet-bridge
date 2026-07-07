package main

import (
	"encoding/binary"
	"image/color"
)

// genIcon generates a 16x16 ICO file with a solid color circle on transparent background.
// Windows systray requires ICO format; this builds one in-memory without any asset files.
//
// ICO layout:
//   ICONDIR (6 bytes) + ICONDIRENTRY (16 bytes) + BITMAPINFOHEADER (40 bytes)
//   + XOR mask (16*16*4 BGRA pixels) + AND mask (16*16/8 bytes)
func genIcon(rgb color.RGBA) []byte {
	const w, h = 16, 16
	const pixelDataSize = w * h * 4
	const andMaskSize = w * h / 8
	const imgSize = 40 + pixelDataSize + andMaskSize
	const totalSize = 6 + 16 + imgSize

	buf := make([]byte, totalSize)
	off := 0

	// ICONDIR
	binary.LittleEndian.PutUint16(buf[off:off+2], 0) // reserved
	binary.LittleEndian.PutUint16(buf[off+2:off+4], 1) // type: icon
	binary.LittleEndian.PutUint16(buf[off+4:off+6], 1) // count: 1 image
	off += 6

	// ICONDIRENTRY
	buf[off+0] = byte(w)       // width (0 means 256)
	buf[off+1] = byte(h)       // height
	buf[off+2] = 0             // color count (0 = ≥256 colors)
	buf[off+3] = 0             // reserved
	binary.LittleEndian.PutUint16(buf[off+4:off+6], 1)   // planes
	binary.LittleEndian.PutUint16(buf[off+6:off+8], 32)  // bit count
	binary.LittleEndian.PutUint32(buf[off+8:off+12], imgSize)    // bytes in resource
	binary.LittleEndian.PutUint32(buf[off+12:off+16], 6+16)      // offset to image data
	off += 16

	// BITMAPINFOHEADER
	binary.LittleEndian.PutUint32(buf[off+0:off+4], 40)        // biSize
	binary.LittleEndian.PutUint32(buf[off+4:off+8], w)         // biWidth
	binary.LittleEndian.PutUint32(buf[off+8:off+12], h*2)      // biHeight (2x for XOR+AND)
	binary.LittleEndian.PutUint16(buf[off+12:off+14], 1)       // biPlanes
	binary.LittleEndian.PutUint16(buf[off+14:off+16], 32)      // biBitCount
	binary.LittleEndian.PutUint32(buf[off+16:off+20], 0)       // biCompression (BI_RGB)
	binary.LittleEndian.PutUint32(buf[off+20:off+24], pixelDataSize) // biSizeImage
	binary.LittleEndian.PutUint32(buf[off+24:off+28], 0)       // biXPelsPerMeter
	binary.LittleEndian.PutUint32(buf[off+28:off+32], 0)       // biYPelsPerMeter
	binary.LittleEndian.PutUint32(buf[off+32:off+36], 0)       // biClrUsed
	binary.LittleEndian.PutUint32(buf[off+36:off+40], 0)       // biClrImportant
	off += 40

	// XOR mask (BGRA pixels, bottom-up). Draw a filled circle.
	cx, cy, r := float64(w)/2, float64(h)/2, float64(w)/2-1
	for y := h - 1; y >= 0; y-- { // bottom-up
		for x := 0; x < w; x++ {
			dx := float64(x) + 0.5 - cx
			dy := float64(y) + 0.5 - cy
			if dx*dx+dy*dy <= r*r {
				// Inside circle: use color
				buf[off+0] = rgb.B
				buf[off+1] = rgb.G
				buf[off+2] = rgb.R
				buf[off+3] = 255 // alpha
			} else {
				// Outside: transparent
				buf[off+0] = 0
				buf[off+1] = 0
				buf[off+2] = 0
				buf[off+3] = 0
			}
			off += 4
		}
	}

	// AND mask (1 bit per pixel, padded to 4-byte rows). 0 = opaque, 1 = transparent.
	// Since we use per-pixel alpha in the XOR mask, set AND mask to all 0 (opaque).
	for i := 0; i < andMaskSize; i++ {
		buf[off+i] = 0
	}

	return buf
}

// State colors for the tray icon.
var (
	iconIdle       = genIcon(color.RGBA{R: 107, G: 114, B: 128, A: 255}) // gray
	iconConnecting = genIcon(color.RGBA{R: 245, G: 158, B: 11, A: 255})  // yellow
	iconRunning    = genIcon(color.RGBA{R: 16, G: 185, B: 129, A: 255})  // green
	iconError      = genIcon(color.RGBA{R: 239, G: 68, B: 68, A: 255})   // red
)
