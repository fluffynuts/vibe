package review

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

var units = []string{"\"\\", "'", "/*", "<!--", "<<<", "#{", "${", "((", "[[", "{{", "a ", "`", "\\\\", "*/", "\"\"\"", "--", "%{", "<a ", "0x", "1e", "@", "$(", "#", "\t", "a\n", "' '", "\"a", "r#\"", "=begin", "<%"}

func tokenise(l chroma.Lexer, text string) time.Duration {
	began := time.Now()
	it, err := chroma.Coalesce(l).Tokenise(nil, text)
	if err != nil {
		return 0
	}
	for tok := it(); tok != chroma.EOF; tok = it() {
	}
	return time.Since(began)
}

func TestZZHighlightCost(t *testing.T) {
	if os.Getenv("ZZ") == "" {
		t.Skip()
	}
	type res struct {
		name, unit string
		small, big time.Duration
	}
	var out []res
	for _, l := range lexers.GlobalLexerRegistry.Lexers {
		for _, u := range units {
			small := tokenise(l, strings.Repeat(u, 4000/len(u)))
			if small < 2*time.Millisecond {
				continue
			}
			big := tokenise(l, strings.Repeat(u, 16000/len(u)))
			out = append(out, res{l.Config().Name, u, small, big})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].big > out[j].big })
	for i, r := range out {
		if i > 25 {
			break
		}
		fmt.Printf("%-25s %-8q 4KB=%-10s 16KB=%-10s x%.1f\n", r.name, r.unit, r.small.Round(time.Millisecond), r.big.Round(time.Millisecond), float64(r.big)/float64(r.small))
	}
}
