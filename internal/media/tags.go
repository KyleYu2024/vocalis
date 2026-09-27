package media

import (
	"errors"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/dhowden/tag"
)

// Embedded holds values read from a file's embedded metadata.
type Embedded struct {
	Title       string
	Artist      string
	Album       string
	AlbumArtist string
	Narrator    string
	Genre       string
	Comment     string
	Year        string
	Track       int
	TrackTotal  int
	Disc        int
	Format      string
	FileType    string

	Picture     []byte
	PictureMIME string
}

// ReadEmbedded parses embedded tags from an audio file. It returns a zero
// value (and no error) when the file simply has no usable tags.
func ReadEmbedded(path string) (*Embedded, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	m, err := tag.ReadFrom(f)
	if err != nil {
		if errors.Is(err, tag.ErrNoTagsFound) || errors.Is(err, io.EOF) {
			return &Embedded{}, nil
		}
		return &Embedded{}, nil
	}

	e := &Embedded{
		Title:       clean(m.Title()),
		Artist:      clean(m.Artist()),
		Album:       clean(m.Album()),
		AlbumArtist: clean(m.AlbumArtist()),
		Genre:       clean(m.Genre()),
		Comment:     clean(m.Comment()),
		Format:      string(m.Format()),
		FileType:    string(m.FileType()),
	}
	if y := m.Year(); y > 0 {
		e.Year = strconv.Itoa(y)
	}
	if t, tt := m.Track(); t > 0 {
		e.Track, e.TrackTotal = t, tt
	}
	if d, _ := m.Disc(); d > 0 {
		e.Disc = d
	}
	if p := m.Picture(); p != nil {
		e.Picture = p.Data
		e.PictureMIME = p.MIMEType
	}
	e.Narrator = narrator(m)
	return e, nil
}

// narrator digs the narrator out of the raw tag map: MP4 files use the "©nrt"
// atom, ID3 files commonly use TXXX/NARRATOR comments.
func narrator(m tag.Metadata) string {
	raw := m.Raw()
	if raw == nil {
		return ""
	}
	for _, k := range []string{"©nrt", "nrt", "NARRATOR", "narrator", "Narrator"} {
		if v, ok := raw[k]; ok {
			if s := clean(toString(v)); s != "" {
				return s
			}
		}
	}
	return ""
}

func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	case int:
		return strconv.Itoa(t)
	default:
		return ""
	}
}

func clean(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\x00", "")
	return s
}
