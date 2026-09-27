package library

import (
	"regexp"
	"strings"
	"unicode"
)

// junkRe matches the release-group noise that surrounds many downloaded
// audiobook folder names, e.g. "【有声书】" or "[MP3 64kbps]".
var junkRe = regexp.MustCompile(`(?i)[【\[（(][^】\]）)]*(有声|小说|完[结本整]|更新[中完]?|连载|全[集本\d]|精校|高清|mp3|m4a|flac|aac|dts|tts|播音|朗读|听书|音频|合集|无删减|超清|多播|双播|单播|ysxs|yousheng)[^】\]）)]*[】\]）)]`)

var trailerRe = regexp.MustCompile(`(?i)\s*[（(\[][^）)\]]*(完[结本整]|更新[中完]?|连载|全\d+[集章]|共\d+[集章]|有声|全集|mp3)[^）)\]]*[）)\]]\s*$`)

// trailerPlainRe strips the same noise when it is not wrapped in brackets,
// e.g. "明朝那些事儿 全集".
var trailerPlainRe = regexp.MustCompile(`(?i)[\s\-_·]*(?:完结|完本|全本|全集|更新中|连载中|有声小说|有声书|有声|全\d+[集章回]|共\d+[集章回]|多播|单播|双播|精校|高清|超清|无删减|mp3|m4a|flac|aac|tts)\s*$`)

// authorFieldRe only fires on a real "作者：X" style marker: either the
// keyword is followed by a colon, or it starts the name and is followed by a
// space. Without that, titles such as "原著书名" would be split in half.
var authorFieldRe = regexp.MustCompile(`(?i)(?:作者|原著|著者|播音|主播|演播|朗读|by)\s*[:：]|^\s*(?:作者|著者|播音|主播|演播|朗读|by)\s+`)

var (
	// 《书名》作者：某某
	bracketTitleRe = regexp.MustCompile(`《([^》]+)》`)
	// 书名（张三 著） / 书名(张三)
	parenAuthorRe = regexp.MustCompile(`[（(]([^）)]{1,24})\s*(?:著|作品|原著)[）)]|^[（(]([^）)]{1,24})[）)]$`)
	// "001. " / "03 - " / "07 " prefixes, but never a bare "第1章".
	leadingIndexRe = regexp.MustCompile(`^\s*\d{1,4}\s*[-–—_.、:：]\s*|^\s*\d{1,4}\s+`)
	discPrefixRe   = regexp.MustCompile(`(?i)^\s*(?:cd|disc|disk|dvd|vol|volume|part|pt|side|碟|盘|卷)\s*[._-]?\s*\d*\s*[._-]?\s*`)
)

// partDirRe matches directory names that describe a slice of one book rather
// than a book of their own, e.g. "CD1", "第二部", "上卷".
var partDirRe = regexp.MustCompile(`(?i)^\s*(?:cd|disc|disk|dvd|vol|volume|part|pt|side|track)\s*[._-]?\s*\d*\s*$|^第\s*[0-9一二三四五六七八九十百零两]+\s*[部卷册集辑]\s*$|^[上中下]\s*[卷部集册]?\s*$|^[碟盘]\s*\d*\s*$|^\s*\d{1,3}\s*$`)

// genericDirRe matches folder names that only say "the audio is in here".
var genericDirRe = regexp.MustCompile(`(?i)^\s*(?:audio|audios|mp3|mp3s?|m4a|flac|audio\s*files|files|media|sound|voice|正文|音频|录音|朗读|内容|正片|全本)\s*$`)

// LooksLikePart reports whether a directory name describes part of a book.
func LooksLikePart(name string) bool {
	s := baseClean(name)
	if partDirRe.MatchString(s) {
		return true
	}
	// "01战虫部队 第1部" also names a part, not a book of its own.
	if trailingPartRe.MatchString(s) {
		return true
	}
	// "CD1 上", "第2部 下" and similar combinations.
	fields := strings.Fields(s)
	if len(fields) < 2 {
		return false
	}
	dirPart, suffixPart := false, false
	for _, f := range fields {
		if partDirRe.MatchString(f) {
			dirPart = true
		}
		if boardSuffixRe.MatchString(f) {
			suffixPart = true
		}
	}
	return dirPart && suffixPart
}

// trailingPartRe matches names that end with a volume marker.
var trailingPartRe = regexp.MustCompile(`第\s*[0-9一二三四五六七八九十百零两]+\s*[部卷册辑篇]\s*$`)

// collectionRe matches folder names that say "this folder is the whole work",
// so several sub folders inside it are chapters, not separate books.
var collectionRe = regexp.MustCompile(`(?i)(全集|合集|全本|完整版|无删减|完结|更新中|全\d+[集章回]|共\d+[集章回])`)

// IsCollectionDir reports whether a folder presents itself as a complete work.
func IsCollectionDir(name string) bool {
	return collectionRe.MatchString(name)
}

// boardSuffixRe matches 上/中/下/上部 style suffixes.
var boardSuffixRe = regexp.MustCompile(`^(?:[上中下](?:[卷部集册])?|[卷部集册][上中下])$`)

func isGenericDir(name string) bool {
	return genericDirRe.MatchString(baseClean(name))
}

// StripJunk removes common release-group decorations from a name.
func StripJunk(name string) string {
	s := name
	for i := 0; i < 3; i++ {
		before := s
		s = junkRe.ReplaceAllString(s, "")
		s = trailerRe.ReplaceAllString(s, "")
		for j := 0; j < 3; j++ {
			trimmed := trailerPlainRe.ReplaceAllString(s, "")
			if trimmed == s {
				break
			}
			s = trimmed
		}
		s = strings.TrimSpace(s)
		s = strings.Trim(s, "_-–— ")
		if s == before {
			break
		}
	}
	return strings.TrimSpace(s)
}

// ParseBookName makes a best effort at splitting a folder or file name into a
// title and an author. Either value may come back empty.
func ParseBookName(raw string) (title, author string) {
	title, author = parseBookName(raw)
	// "作者"/"播音" and friends are field labels, not names.
	if isGenericAuthor(author) {
		author = ""
	}
	return title, author
}

func parseBookName(raw string) (title, author string) {
	s := StripJunk(raw)
	if s == "" {
		return strings.TrimSpace(raw), ""
	}
	s = stripFolderIndex(s)
	if title, author, ok := splitAtAuthorMarker(s); ok {
		return title, author
	}

	// 《书名》
	if m := bracketTitleRe.FindStringSubmatch(s); m != nil {
		title = strings.TrimSpace(m[1])
		rest := strings.TrimSpace(bracketTitleRe.ReplaceAllString(s, " "))
		return title, parseAuthorFrom(rest)
	}

	// 书名（作者 著）
	if m := parenAuthorRe.FindStringSubmatch(s); m != nil {
		name := m[1]
		if name == "" {
			name = m[2]
		}
		name = strings.TrimSpace(name)
		if name != "" {
			t := strings.TrimSpace(parenAuthorRe.ReplaceAllString(s, ""))
			t = strings.TrimSpace(strings.Trim(t, "-–—_ "))
			if t != "" {
				return t, name
			}
		}
	}

	// "A - B" style names.
	if parts := splitDash(s); len(parts) == 2 {
		l, r := parts[0], parts[1]
		lp, rp := looksLikePersonName(l), looksLikePersonName(r)
		if lp != rp {
			if lp {
				return r, l
			}
			return l, r
		}
		lw, rw := len(strings.Fields(l)), len(strings.Fields(r))
		if lw <= 2 && rw >= 4 {
			return r, l
		}
		if rw <= 2 && lw >= 4 {
			return l, r
		}
		return l, r
	}

	return strings.TrimSpace(s), ""
}

// stripFolderIndex removes a leading series index such as "01" in
// "01上下五千年" or "007. 三体". Plain titles like "1984" or "007" are kept.
func stripFolderIndex(s string) string {
	if m := leadingIndexRe.FindString(s); m != "" {
		if rest := strings.TrimSpace(s[len(m):]); rest != "" {
			return rest
		}
		return s
	}
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	// A leading zero is a strong hint that the digits are a series index, but
	// only when there is a real title behind them.
	if i >= 2 && i < len(s) && s[0] == '0' {
		if rest := strings.TrimSpace(s[i:]); rest != "" {
			return rest
		}
	}
	return s
}

var genericAuthors = map[string]bool{
	"作者": true, "著者": true, "原著": true, "播音": true,
	"主播": true, "演播": true, "朗读": true, "未知": true, "佚名": true,
}

func isGenericAuthor(s string) bool {
	s = strings.TrimSpace(s)
	return genericAuthors[s]
}

// splitAtAuthorMarker handles "书名 作者：某某" and "作者：某某 书名".
func splitAtAuthorMarker(s string) (title, author string, ok bool) {
	idx := authorFieldRe.FindStringIndex(s)
	if idx == nil {
		return "", "", false
	}
	dupIdx := authorFieldRe.FindAllStringIndex(s, -1)
	head := unwrapTitle(cleanAuthorSide(s[:idx[0]]))
	tail := unwrapTitle(cleanAuthorSide(s[idx[1]:]))
	if len(dupIdx) > 1 {
		// Cut the author part off before the next marker, e.g. "作者：X 播音：Y".
		if next := dupIdx[1]; next[0] >= idx[1] {
			tail = unwrapTitle(cleanAuthorSide(s[idx[1]:next[0]]))
		}
	}
	switch {
	case head != "" && tail != "":
		return head, tail, true
	case tail != "":
		return tail, "", true
	case head != "":
		return head, "", true
	}
	return "", "", false
}

func cleanAuthorSide(s string) string {
	s = StripJunk(s)
	s = strings.TrimSpace(strings.Trim(strings.TrimSpace(s), "-–—_·"))
	return s
}

// unwrapTitle strips 书名号 from a title.
func unwrapTitle(s string) string {
	if m := bracketTitleRe.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[1])
	}
	return s
}

func parseAuthorFrom(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if idx := authorFieldRe.FindStringIndex(s); idx != nil {
		s = s[idx[1]:]
	}
	s = StripJunk(s)
	s = strings.TrimSpace(strings.Trim(s, "-–—_ "))
	if strings.HasPrefix(s, "by ") || strings.HasPrefix(s, "BY ") {
		s = strings.TrimSpace(s[3:])
	}
	return s
}

func splitDash(s string) []string {
	for _, sep := range []string{" - ", " – ", " — ", "-", "|", "_"} {
		if parts := strings.Split(s, sep); len(parts) == 2 {
			a, b := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
			if a != "" && b != "" {
				return []string{a, b}
			}
		}
	}
	return nil
}

// commonSurnames holds the Chinese surnames that most often start an author
// name, used to tell "作者 - 书名" from "书名 - 作者".
const commonSurnames = `赵钱孙李周吴郑王冯陈褚卫蒋沈韩杨朱秦尤许何吕施张孔曹严华金魏陶姜戚谢邹喻柏窦章云苏潘葛范彭郎鲁韦昌马苗凤花方俞任袁柳鲍史唐费廉岑薛雷贺倪汤滕殷罗毕郝安常乐于傅卞齐康伍余元顾孟平黄和穆萧尹姚邵汪祁毛禹狄贝明臧计伏成戴谈宋茅庞熊纪舒屈项祝董梁杜阮蓝闵席季麻强贾路娄危江童颜郭梅盛林钟徐邱骆高夏蔡田樊胡凌霍虞万柯管卢莫房裘解应宗丁宣邓郁单杭洪包诸左石崔吉龚程邢裴陆荣翁荀羊惠甄曲封靳松井段富巫乌焦巴弓牧山谷车侯全班仰秋仲伊宫宁仇栾暴甘厉戎祖武符刘景詹束龙叶幸司韶黎薄印宿白怀蒲邰鄂索咸赖卓蔺屠蒙池乔胥苍双闻莘党翟谭贡劳姬申扶堵冉雍桑桂濮牛寿通边扈燕冀浦尚农温别庄晏柴瞿阎充慕连茹习艾鱼容向古易慎戈廖庾居衡步都耿满弘匡国文寇广东欧沃利越隆师巩聂晁勾融冷辛那简饶曾沙养鞠须丰巢关蒯相查后荆红游竺权逯盖益桓`

// looksLikePersonName is a heuristic for deciding which half of "A - B" is
// the author.
func looksLikePersonName(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	runes := []rune(s)
	if len(runes) < 2 || len(runes) > 20 {
		return false
	}
	if strings.ContainsAny(s, "·‧") {
		return true
	}
	allHan := true
	for _, c := range runes {
		if !unicode.Is(unicode.Han, c) {
			allHan = false
			break
		}
	}
	if allHan {
		if len(runes) > 4 {
			return false
		}
		return strings.ContainsRune(commonSurnames, runes[0])
	}
	// Latin script: a single word, or something with initials, reads as a name.
	for _, c := range runes {
		if !unicode.IsLetter(c) && c != '.' && c != ' ' && c != '-' && c != '\'' {
			return false
		}
	}
	words := strings.Fields(s)
	return len(words) == 1 || strings.Contains(s, ".")
}

// ChapterTitle turns a file name into something readable for the podcast app.
func ChapterTitle(fileBase string) string {
	base := strings.TrimSpace(strings.TrimSuffix(fileBase, filepathExt(fileBase)))
	if base == "" {
		return fileBase
	}
	s := base
	s = StripJunk(s)
	s = discPrefixRe.ReplaceAllString(s, "")
	s = leadingIndexRe.ReplaceAllString(s, "")
	s = strings.TrimSpace(strings.Trim(s, "-–—_ ."))
	if s == "" {
		return base
	}
	return s
}

func filepathExt(name string) string {
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		return name[i:]
	}
	return ""
}

func baseClean(name string) string {
	return strings.TrimSpace(strings.ToLower(name))
}
