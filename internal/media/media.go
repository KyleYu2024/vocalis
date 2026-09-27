// Package media reads embedded audio metadata (tags, cover art) and computes
// track durations without any external binaries.
package media

import (
	"path/filepath"
	"strings"
)

// ExtMIME maps a lowercase file extension to a MIME type.
var ExtMIME = map[string]string{
	".mp3":  "audio/mpeg",
	".m4a":  "audio/mp4",
	".m4b":  "audio/mp4",
	".m4p":  "audio/mp4",
	".aac":  "audio/aac",
	".flac": "audio/flac",
	".ogg":  "audio/ogg",
	".oga":  "audio/ogg",
	".opus": "audio/ogg",
	".wav":  "audio/wav",
	".wma":  "audio/x-ms-wma",
	".alac": "audio/mp4",
	".dsf":  "audio/x-dsf",
	".m4r":  "audio/mp4",
}

// ImageExtMIME maps an image extension to a MIME type.
var ImageExtMIME = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
	".gif":  "image/gif",
	".bmp":  "image/bmp",
}

// IsAudio reports whether the path looks like a supported audio file.
func IsAudio(path string) bool {
	_, ok := ExtMIME[strings.ToLower(filepath.Ext(path))]
	return ok
}

// IsImage reports whether the path looks like a supported image file.
func IsImage(path string) bool {
	_, ok := ImageExtMIME[strings.ToLower(filepath.Ext(path))]
	return ok
}

// MIMEFor returns the MIME type for a file path, defaulting to
// application/octet-stream.
func MIMEFor(path string) string {
	if m, ok := ExtMIME[strings.ToLower(filepath.Ext(path))]; ok {
		return m
	}
	if m, ok := ImageExtMIME[strings.ToLower(filepath.Ext(path))]; ok {
		return m
	}
	return "application/octet-stream"
}
