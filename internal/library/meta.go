package library

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// LocalMeta is the optional hand written metadata file that may live inside a
// book folder. It always wins over values guessed from names or audio tags.
type LocalMeta struct {
	Title       string   `json:"title"`
	Author      string   `json:"author"`
	Narrator    string   `json:"narrator"`
	Description string   `json:"description"`
	Publisher   string   `json:"publisher"`
	Year        string   `json:"year"`
	Language    string   `json:"language"`
	Series      string   `json:"series"`
	SeriesIndex int      `json:"seriesIndex"`
	Genres      []string `json:"genres"`
	Cover       string   `json:"cover"`
}

// metaFileNames are checked in order; the first readable one wins.
// ".podshelf.json" is the pre-rename name and still works.
var metaFileNames = []string{".vocalis.json", ".podshelf.json", "book.json", "metadata.json"}

func readLocalMeta(dir string) (*LocalMeta, string) {
	for _, name := range metaFileNames {
		p := filepath.Join(dir, name)
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var m LocalMeta
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		return &m, p
	}
	return nil, ""
}

func nowUTC() time.Time { return time.Now().UTC() }
