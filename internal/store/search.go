package store

import (
	"strings"

	"sort"
	"vocalis/internal/model"
	"vocalis/internal/natsort"
)

// ChapterMatch is one chapter that matched a query.
type ChapterMatch struct {
	Book    *model.Book
	Chapter model.Chapter
}

// SearchResult is what the search box returns.
type SearchResult struct {
	Query string
	Books []*model.Book
	// Chapters holds at most Limit hits; ChapterTotal is how many matched.
	Chapters     []ChapterMatch
	ChapterTotal int
}

// Search finds books and chapters matching every whitespace separated term.
// Matching is case insensitive and simply looks for substrings, which is what
// people expect when typing part of a Chinese title.
func (s *Store) Search(query string, limit int) SearchResult {
	if limit <= 0 {
		limit = 200
	}
	res := SearchResult{Query: strings.TrimSpace(query)}
	terms := strings.Fields(strings.ToLower(res.Query))
	if len(terms) == 0 {
		return res
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	books := make([]*model.Book, 0, len(s.books))
	for _, b := range s.books {
		books = append(books, b)
	}
	sort.Slice(books, func(i, j int) bool { return natsort.Less(books[i].RelDir, books[j].RelDir) })

	for _, b := range books {
		bookHay := strings.ToLower(strings.Join([]string{
			b.Title, b.Author, b.Narrator, b.Publisher, b.Series, b.RelDir,
		}, " "))
		if matchAll(bookHay, terms) {
			res.Books = append(res.Books, cloneBook(b))
		}
		for i := range b.Chapters {
			c := b.Chapters[i]
			hay := strings.ToLower(c.Title + " " + c.RelPath)
			if !matchAll(hay, terms) {
				continue
			}
			res.ChapterTotal++
			if len(res.Chapters) < limit {
				res.Chapters = append(res.Chapters, ChapterMatch{
					Book:    cloneBook(b),
					Chapter: c,
				})
			}
		}
	}
	return res
}

func matchAll(haystack string, terms []string) bool {
	for _, t := range terms {
		if !strings.Contains(haystack, t) {
			return false
		}
	}
	return true
}
