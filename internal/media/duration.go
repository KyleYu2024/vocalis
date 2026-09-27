package media

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var errUnsupported = errors.New("media: unsupported container for duration")

// Duration returns the playtime of an audio file in seconds, or 0 when it
// cannot be determined. It is best effort and never returns an error to the
// caller.
func Duration(path string) float64 {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		if d, err := mp3Duration(path); err == nil {
			return d
		}
	case ".m4a", ".m4b", ".m4p", ".m4r", ".mp4", ".aac", ".alac":
		if d, err := mp4Duration(path); err == nil {
			return d
		}
	case ".flac":
		if d, err := flacDuration(path); err == nil {
			return d
		}
	case ".wav":
		if d, err := wavDuration(path); err == nil {
			return d
		}
	}
	return 0
}

// ---------------------------------------------------------------- MPEG audio

var (
	mpeg1Bitrate = map[int][]int{
		1: {0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448},
		2: {0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384},
		3: {0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320},
	}
	mpeg2Bitrate = map[int][]int{
		1: {0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256},
		2: {0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160},
		3: {0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160},
	}
)

type mp3Header struct {
	version     int // 1, 2 or 25 (MPEG2.5)
	layer       int // 1, 2 or 3
	bitrate     int // kbit/s
	sampleRate  int
	mono        bool
	samplesSize int
}

func parseMP3Header(b []byte) (mp3Header, bool) {
	var h mp3Header
	if len(b) < 4 || b[0] != 0xFF || b[1]&0xE0 != 0xE0 {
		return h, false
	}
	verBits := (b[1] >> 3) & 0x03
	switch verBits {
	case 0:
		h.version = 25
	case 2:
		h.version = 2
	case 3:
		h.version = 1
	default:
		return h, false
	}
	h.layer = 4 - int((b[1]>>1)&0x03)
	if h.layer < 1 || h.layer > 3 {
		return h, false
	}
	brIdx := int(b[2] >> 4)
	srIdx := int((b[2] >> 2) & 0x03)
	if brIdx == 0 || brIdx == 15 || srIdx == 3 {
		return h, false
	}
	if h.version == 1 {
		h.bitrate = mpeg1Bitrate[h.layer][brIdx]
		h.sampleRate = []int{44100, 48000, 32000}[srIdx]
	} else {
		h.bitrate = mpeg2Bitrate[h.layer][brIdx]
		if h.version == 2 {
			h.sampleRate = []int{22050, 24000, 16000}[srIdx]
		} else {
			h.sampleRate = []int{11025, 12000, 8000}[srIdx]
		}
	}
	h.mono = (b[3]>>6)&0x03 == 3
	switch h.layer {
	case 1:
		h.samplesSize = 384
	case 2:
		h.samplesSize = 1152
	default:
		if h.version == 1 {
			h.samplesSize = 1152
		} else {
			h.samplesSize = 576
		}
	}
	return h, h.bitrate > 0 && h.sampleRate > 0
}

func mp3Duration(path string) (float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	total := st.Size()

	start, err := id3v2Size(f)
	if err != nil {
		return 0, err
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return 0, err
	}
	buf := make([]byte, 16*1024)
	n, _ := io.ReadFull(f, buf)
	buf = buf[:n]

	off := -1
	var h mp3Header
	for i := 0; i+4 <= len(buf); i++ {
		if hh, ok := parseMP3Header(buf[i:]); ok {
			off, h = i, hh
			break
		}
	}
	if off < 0 {
		return 0, errUnsupported
	}

	// Look for a VBR header that gives us an exact frame count.
	sideInfo := 36
	if h.mono {
		sideInfo = 21
	}
	if h.version != 1 {
		sideInfo = 21
		if h.mono {
			sideInfo = 13
		}
	}
	if off+sideInfo+16 <= len(buf) {
		tag := string(buf[off+sideInfo : off+sideInfo+4])
		if tag == "Xing" || tag == "Info" {
			flags := binary.BigEndian.Uint32(buf[off+sideInfo+4:])
			if flags&0x01 != 0 {
				frames := binary.BigEndian.Uint32(buf[off+sideInfo+8:])
				if frames > 0 {
					return float64(frames) * float64(h.samplesSize) / float64(h.sampleRate), nil
				}
			}
		}
	}
	if off+46 <= len(buf) && string(buf[off+32:off+36]) == "VBRI" {
		frames := binary.BigEndian.Uint32(buf[off+32+14:])
		if frames > 0 {
			return float64(frames) * float64(h.samplesSize) / float64(h.sampleRate), nil
		}
	}

	// Constant bitrate estimate over the remaining audio bytes.
	audio := total - (int64(off) + start)
	if audio <= 0 {
		return 0, errUnsupported
	}
	return float64(audio) * 8 / float64(h.bitrate*1000), nil
}

// id3v2Size returns the byte offset of the first audio frame, skipping an
// ID3v2 header (and its optional footer) when present.
func id3v2Size(f *os.File) (int64, error) {
	var hdr [10]byte
	if _, err := f.ReadAt(hdr[:], 0); err != nil {
		return 0, err
	}
	if string(hdr[0:3]) != "ID3" {
		return 0, nil
	}
	size := int64(hdr[6]&0x7F)<<21 | int64(hdr[7]&0x7F)<<14 | int64(hdr[8]&0x7F)<<7 | int64(hdr[9]&0x7F)
	off := int64(10) + size
	if hdr[5]&0x10 != 0 { // footer present
		off += 10
	}
	return off, nil
}

// ------------------------------------------------------------------ MP4 / M4A

func mp4Duration(path string) (float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	timescale, duration, err := findMvhd(f, 0, st.Size())
	if err != nil {
		return 0, err
	}
	if timescale == 0 {
		return 0, errUnsupported
	}
	return float64(duration) / float64(timescale), nil
}

func findMvhd(f *os.File, start, size int64) (uint32, uint64, error) {
	pos := start
	end := start + size
	var hdr [8]byte
	for pos+8 <= end {
		if _, err := f.ReadAt(hdr[:], pos); err != nil {
			return 0, 0, err
		}
		boxSize := int64(binary.BigEndian.Uint32(hdr[0:4]))
		typ := string(hdr[4:8])
		headerLen := int64(8)
		switch boxSize {
		case 1:
			var ext [8]byte
			if _, err := f.ReadAt(ext[:], pos+8); err != nil {
				return 0, 0, err
			}
			boxSize = int64(binary.BigEndian.Uint64(ext[:]))
			headerLen = 16
		case 0:
			boxSize = end - pos // extends to the end of the file
		}
		if boxSize < headerLen || pos+boxSize > end {
			return 0, 0, errUnsupported
		}
		switch typ {
		case "mvhd":
			body := make([]byte, 120)
			n, _ := f.ReadAt(body, pos+headerLen)
			body = body[:n]
			if len(body) < 20 {
				return 0, 0, errUnsupported
			}
			if body[0] == 1 {
				if len(body) < 32 {
					return 0, 0, errUnsupported
				}
				ts := binary.BigEndian.Uint32(body[20:24])
				dur := binary.BigEndian.Uint64(body[24:32])
				return ts, dur, nil
			}
			ts := binary.BigEndian.Uint32(body[12:16])
			dur := uint64(binary.BigEndian.Uint32(body[16:20]))
			return ts, dur, nil
		case "moov":
			ts, dur, err := findMvhd(f, pos+headerLen, boxSize-headerLen)
			if err == nil {
				return ts, dur, nil
			}
		}
		pos += boxSize
	}
	return 0, 0, errUnsupported
}

// ---------------------------------------------------------------------- FLAC

func flacDuration(path string) (float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var hdr [4]byte
	if _, err := f.ReadAt(hdr[:], 0); err != nil {
		return 0, err
	}
	if string(hdr[:]) != "fLaC" {
		return 0, errUnsupported
	}
	// STREAMINFO is the first metadata block and is always 34 bytes long.
	var block [4 + 34]byte
	if _, err := f.ReadAt(block[:], 4); err != nil {
		return 0, err
	}
	if block[0]&0x7F != 0 {
		return 0, errUnsupported
	}
	info := binary.BigEndian.Uint64(block[4+10 : 4+18])
	sampleRate := info >> 44
	totalSamples := info & 0xFFFFFFFFF
	if sampleRate == 0 || totalSamples == 0 {
		return 0, errUnsupported
	}
	return float64(totalSamples) / float64(sampleRate), nil
}

// ----------------------------------------------------------------------- WAV

func wavDuration(path string) (float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var riff [12]byte
	if _, err := f.ReadAt(riff[:], 0); err != nil {
		return 0, err
	}
	if string(riff[0:4]) != "RIFF" || string(riff[8:12]) != "WAVE" {
		return 0, errUnsupported
	}
	pos := int64(12)
	var byteRate uint32
	var dataSize uint32
	var hdr [8]byte
	for {
		if _, err := f.ReadAt(hdr[:], pos); err != nil {
			break
		}
		id := string(hdr[0:4])
		size := int64(binary.LittleEndian.Uint32(hdr[4:8]))
		switch id {
		case "fmt ":
			var fmtBuf [16]byte
			if _, err := f.ReadAt(fmtBuf[:], pos+8); err != nil {
				return 0, err
			}
			byteRate = binary.LittleEndian.Uint32(fmtBuf[8:12])
		case "data":
			dataSize = uint32(size)
		}
		if size < 0 {
			break
		}
		pos += 8 + size + size%2
	}
	if byteRate == 0 || dataSize == 0 {
		return 0, errUnsupported
	}
	return float64(dataSize) / float64(byteRate), nil
}
