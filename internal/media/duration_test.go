package media

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// xingMP3 builds a minimal MPEG1 Layer III file with a Xing VBR header, which
// is how most audiobook rippers store the exact frame count.
func xingMP3(frames int) []byte {
	b := []byte{0xFF, 0xFB, 0x90, 0x00} // MPEG1 L3, 128 kbps, 44.1 kHz, stereo
	b = append(b, make([]byte, 32)...)  // side information
	b = append(b, "Xing"...)
	b = append(b, 0, 0, 0, 1) // flags: frame count present
	b = binary.BigEndian.AppendUint32(b, uint32(frames))
	b = append(b, make([]byte, 5000)...)
	return b
}

func TestDurationMP3WithXing(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.mp3")
	if err := os.WriteFile(p, xingMP3(1000), 0o644); err != nil {
		t.Fatal(err)
	}
	want := 1000 * 1152.0 / 44100.0
	if got := Duration(p); math.Abs(got-want) > 0.01 {
		t.Fatalf("Duration = %.3f, want %.3f", got, want)
	}
}

func TestDurationMP3ConstantBitrate(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "b.mp3")
	b := []byte{0xFF, 0xFB, 0x90, 0x00}
	b = append(b, make([]byte, 32000)...)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	want := float64(len(b)) * 8 / 128000.0
	if got := Duration(p); math.Abs(got-want) > 0.01 {
		t.Fatalf("Duration = %.3f, want %.3f", got, want)
	}
}

func TestDurationMP3SkipsID3v2(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.mp3")
	id3 := []byte{'I', 'D', '3', 3, 0, 0, 0, 0, 0, 100}
	body := append(id3, make([]byte, 100)...)
	body = append(body, xingMP3(500)...)
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	want := 500 * 1152.0 / 44100.0
	if got := Duration(p); math.Abs(got-want) > 0.01 {
		t.Fatalf("Duration = %.3f, want %.3f", got, want)
	}
}

func TestDurationWAV(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.wav")
	var b []byte
	b = append(b, "RIFF"...)
	b = binary.LittleEndian.AppendUint32(b, 0)
	b = append(b, "WAVE"...)
	b = append(b, "fmt "...)
	b = binary.LittleEndian.AppendUint32(b, 16)
	b = binary.LittleEndian.AppendUint16(b, 1) // PCM
	b = binary.LittleEndian.AppendUint16(b, 1) // mono
	b = binary.LittleEndian.AppendUint32(b, 44100)
	b = binary.LittleEndian.AppendUint32(b, 88200) // byte rate
	b = binary.LittleEndian.AppendUint16(b, 2)
	b = binary.LittleEndian.AppendUint16(b, 16)
	b = append(b, "data"...)
	b = binary.LittleEndian.AppendUint32(b, 88200) // one second of audio
	b = append(b, make([]byte, 88200)...)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Duration(p); math.Abs(got-1.0) > 0.01 {
		t.Fatalf("Duration = %.3f, want 1.000", got)
	}
}

func TestDurationFLAC(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.flac")
	b := []byte("fLaC")
	b = append(b, 0x00, 0x00, 0x00, 0x22) // STREAMINFO, 34 bytes
	info := make([]byte, 34)
	packed := uint64(44100)<<44 | uint64(44100*3) // 3 seconds at 44.1 kHz
	binary.BigEndian.PutUint64(info[10:18], packed)
	b = append(b, info...)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Duration(p); math.Abs(got-3.0) > 0.01 {
		t.Fatalf("Duration = %.3f, want 3.000", got)
	}
}

func TestDurationM4A(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.m4a")

	mvhd := make([]byte, 8+20)
	binary.BigEndian.PutUint32(mvhd[0:4], uint32(len(mvhd)))
	copy(mvhd[4:8], "mvhd")
	binary.BigEndian.PutUint32(mvhd[8+12:8+16], 1000) // timescale
	binary.BigEndian.PutUint32(mvhd[8+16:8+20], 7500) // duration -> 7.5s

	moov := make([]byte, 8+len(mvhd))
	binary.BigEndian.PutUint32(moov[0:4], uint32(len(moov)))
	copy(moov[4:8], "moov")
	copy(moov[8:], mvhd)

	ftyp := make([]byte, 16)
	binary.BigEndian.PutUint32(ftyp[0:4], 16)
	copy(ftyp[4:8], "ftyp")
	copy(ftyp[8:12], "M4A ")

	if err := os.WriteFile(p, append(ftyp, moov...), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Duration(p); math.Abs(got-7.5) > 0.01 {
		t.Fatalf("Duration = %.3f, want 7.500", got)
	}
}

func TestDurationUnknownFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.ogg")
	if err := os.WriteFile(p, []byte("not really audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Duration(p); got != 0 {
		t.Fatalf("Duration = %v, want 0", got)
	}
}
