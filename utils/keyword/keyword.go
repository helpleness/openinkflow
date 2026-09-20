package keyword

import (
	"strings"
	"sync"
	"unicode"

	"github.com/go-ego/gse"
)

var stopwordList = []string{
	"一个", "一下", "什么", "为什么", "怎么", "如何", "请问", "请",
	"这个", "那个", "当前", "现在", "正在", "需要", "相关", "介绍",
	"the", "and", "for", "with", "what", "how", "why",
}

var stopwords = makeStopwords(stopwordList)

var (
	segmenter     gse.Segmenter
	segmenterOnce sync.Once
)

func makeStopwords(words []string) map[string]struct{} {
	out := make(map[string]struct{}, len(words))
	for _, word := range words {
		out[word] = struct{}{}
	}
	return out
}

func sharedSegmenter() *gse.Segmenter {
	segmenterOnce.Do(func() {
		if err := segmenter.LoadDictEmbed("zh_s"); err != nil {
			_ = segmenter.LoadDictEmbed()
		}
		segmenter.LoadStopArr(stopwordList)
	})
	return &segmenter
}

func Normalize(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, 48)
	add := func(term string) {
		term = strings.TrimSpace(term)
		if term == "" {
			return
		}
		if _, stop := stopwords[strings.ToLower(term)]; stop {
			return
		}
		if seen[term] {
			return
		}
		seen[term] = true
		out = append(out, term)
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if len([]rune(value)) <= 16 {
			add(value)
		}
		for _, term := range Split(value) {
			add(term)
			if len(out) >= 48 {
				return out
			}
		}
	}
	return out
}

func Split(text string) []string {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) ||
			strings.ContainsRune("，。！？；：、“”‘’（）《》【】…·|/\\", r)
	})
	out := make([]string, 0, len(fields)*4)
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		runes := []rune(field)
		fieldHasHan := HasHan(field)
		if (!fieldHasHan && len(runes) <= 32) || len(runes) <= 16 {
			out = append(out, field)
		}
		out = append(out, sharedSegmenter().CutSearch(field, true)...)
		if fieldHasHan && len(runes) > 2 {
			for n := 2; n <= 4; n++ {
				if len(runes) < n {
					continue
				}
				for i := 0; i+n <= len(runes); i++ {
					out = append(out, string(runes[i:i+n]))
				}
			}
		}
	}
	return out
}

func HasHan(text string) bool {
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func BuildQueryText(keywords []string, maxRunes int) string {
	var b strings.Builder
	for _, term := range keywords {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(term)
		if maxRunes > 0 && len([]rune(b.String())) >= maxRunes {
			break
		}
	}
	return strings.TrimSpace(b.String())
}
