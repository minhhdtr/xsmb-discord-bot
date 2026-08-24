// Package htmlscan pulls element text out of HTML by CSS class.
//
// Not a general parser. It does one job, and it is written to survive the
// markup scraping targets actually emit: unclosed cells, unquoted
// attributes, stray end tags, truncated documents.
package htmlscan

import (
	"strconv"
	"strings"
)

// voidElements never have children and never need an end tag.
var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true,
	"param": true, "source": true, "track": true, "wbr": true,
}

// Tags a browser implicitly closes when the same tag opens again. Lottery
// tables are full of `<td>1<td>2` with no closing tags.
var selfClosingOnRepeat = map[string]bool{
	"td": true, "th": true, "tr": true, "li": true, "p": true,
	"option": true, "dt": true, "dd": true,
}

// rawTextElements have contents that must not be read as markup.
var rawTextElements = map[string]bool{"script": true, "style": true}

// Block-level tags become a space in captured text, so a cell holding
// `02502<br>30375` reads as two numbers. Inline tags deliberately don't:
// `<span>94</span><span>533</span>` is one number, 94533.
var blockLevel = map[string]bool{
	"address": true, "article": true, "aside": true, "blockquote": true,
	"br": true, "dd": true, "div": true, "dl": true, "dt": true,
	"fieldset": true, "figure": true, "footer": true, "form": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"header": true, "hr": true, "li": true, "main": true, "nav": true,
	"ol": true, "p": true, "pre": true, "section": true, "table": true,
	"tbody": true, "td": true, "tfoot": true, "th": true, "thead": true,
	"tr": true, "ul": true,
}

// TextByClass returns the text of every element carrying class, in document
// order. Nested matches aren't reported separately.
func TextByClass(html, class string) []string {
	s := &scanner{src: html, class: class}
	s.run()
	return s.out
}

type scanner struct {
	src   string
	class string
	pos   int

	stack []string
	out   []string

	capturing bool
	rootIndex int
	buf       strings.Builder
}

func (s *scanner) run() {
	for s.pos < len(s.src) {
		next := strings.IndexByte(s.src[s.pos:], '<')
		if next < 0 {
			s.text(s.src[s.pos:])
			s.pos = len(s.src)
			break
		}
		s.text(s.src[s.pos : s.pos+next])
		s.pos += next
		s.tag()
	}
	// A truncated document should still yield what was captured.
	s.emit()
}

func (s *scanner) text(chunk string) {
	if s.capturing && chunk != "" {
		s.buf.WriteString(chunk)
	}
}

// tag consumes one construct starting at s.src[s.pos] == '<'.
func (s *scanner) tag() {
	rest := s.src[s.pos:]
	switch {
	case strings.HasPrefix(rest, "<!--"):
		s.skipUntil("-->", 3)
		return
	case strings.HasPrefix(rest, "<!"), strings.HasPrefix(rest, "<?"):
		s.skipUntil(">", 1)
		return
	}

	closing := strings.HasPrefix(rest, "</")
	nameStart := 1
	if closing {
		nameStart = 2
	}
	name, after := readName(rest, nameStart)
	if name == "" { // a bare '<' in text, not a tag
		s.text("<")
		s.pos++
		return
	}

	attrs, end, selfClosed := readAttributes(rest, after)
	s.pos += end

	if closing {
		s.closeTag(name)
		return
	}
	s.openTag(name, attrs, selfClosed)
}

func (s *scanner) openTag(name, attrs string, selfClosed bool) {
	s.separate(name)
	if voidElements[name] || selfClosed {
		return // nothing to push; contributes no text
	}
	if rawTextElements[name] {
		s.skipRawText(name)
		return
	}
	if selfClosingOnRepeat[name] && len(s.stack) > 0 && s.stack[len(s.stack)-1] == name {
		s.closeTag(name)
	}
	s.stack = append(s.stack, name)
	if !s.capturing && hasClass(attrs, s.class) {
		s.capturing = true
		s.rootIndex = len(s.stack) - 1
		s.buf.Reset()
	}
}

func (s *scanner) closeTag(name string) {
	s.separate(name)
	index := -1
	for i := len(s.stack) - 1; i >= 0; i-- {
		if s.stack[i] == name {
			index = i
			break
		}
	}
	if index < 0 {
		return // stray end tag
	}
	if s.capturing && s.rootIndex >= index {
		s.emit()
	}
	s.stack = s.stack[:index]
}

// separate marks a visual break. collapse() squeezes runs of them later.
func (s *scanner) separate(name string) {
	if s.capturing && blockLevel[name] {
		s.buf.WriteByte(' ')
	}
}

func (s *scanner) emit() {
	if !s.capturing {
		return
	}
	s.capturing = false
	s.out = append(s.out, collapse(decodeEntities(s.buf.String())))
	s.buf.Reset()
}

func (s *scanner) skipUntil(marker string, minAdvance int) {
	at := strings.Index(s.src[s.pos+minAdvance:], marker)
	if at < 0 {
		s.pos = len(s.src)
		return
	}
	s.pos += minAdvance + at + len(marker)
}

// skipRawText discards <script>/<style> bodies without reading them as markup.
func (s *scanner) skipRawText(name string) {
	lower := strings.ToLower(s.src[s.pos:])
	at := strings.Index(lower, "</"+name)
	if at < 0 {
		s.pos = len(s.src)
		return
	}
	s.pos += at
	rest := s.src[s.pos:]
	_, after := readName(rest, 2)
	_, end, _ := readAttributes(rest, after)
	s.pos += end
}

// readName reads a tag name, lowercased, and returns the offset after it.
func readName(s string, from int) (string, int) {
	i := from
	for i < len(s) && isNameByte(s[i]) {
		i++
	}
	if i == from {
		return "", from
	}
	return strings.ToLower(s[from:i]), i
}

func isNameByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '-' || b == '_' || b == ':'
}

// readAttributes scans to the tag's '>', respecting quotes so a '>' inside an
// attribute doesn't end the tag early.
func readAttributes(s string, from int) (attrs string, end int, selfClosed bool) {
	i := from
	var quote byte
	for i < len(s) {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '>':
			raw := s[from:i]
			trimmed := strings.TrimRight(raw, " \t\r\n")
			if strings.HasSuffix(trimmed, "/") {
				return strings.TrimSuffix(trimmed, "/"), i + 1, true
			}
			return raw, i + 1, false
		}
		i++
	}
	return s[from:], len(s), false // truncated input
}

// hasClass reports whether the class attribute contains want as a whole token.
func hasClass(attrs, want string) bool {
	value, ok := attributeValue(attrs, "class")
	if !ok {
		return false
	}
	for _, token := range strings.Fields(value) {
		if token == want {
			return true
		}
	}
	return false
}

// attributeValue accepts double, single and unquoted values. data-class= is
// not class=.
func attributeValue(attrs, name string) (string, bool) {
	i := 0
	for i < len(attrs) {
		for i < len(attrs) && isSpace(attrs[i]) {
			i++
		}
		start := i
		for i < len(attrs) && !isSpace(attrs[i]) && attrs[i] != '=' {
			i++
		}
		key := strings.ToLower(attrs[start:i])
		for i < len(attrs) && isSpace(attrs[i]) {
			i++
		}
		if i >= len(attrs) || attrs[i] != '=' {
			if key == name {
				return "", true // valueless attribute
			}
			continue
		}
		i++
		for i < len(attrs) && isSpace(attrs[i]) {
			i++
		}
		var value string
		if i < len(attrs) && (attrs[i] == '"' || attrs[i] == '\'') {
			quote := attrs[i]
			i++
			vs := i
			for i < len(attrs) && attrs[i] != quote {
				i++
			}
			value = attrs[vs:i]
			if i < len(attrs) {
				i++
			}
		} else {
			vs := i
			for i < len(attrs) && !isSpace(attrs[i]) {
				i++
			}
			value = attrs[vs:i]
		}
		if key == name {
			return value, true
		}
	}
	return "", false
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n' || b == '\f'
}

// collapse trims and squeezes runs of whitespace to a single space.
func collapse(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isSpace(c) {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteByte(c)
	}
	return b.String()
}

var namedEntities = map[string]string{
	"nbsp": "\u00a0", "amp": "&", "lt": "<", "gt": ">",
	"quot": "\"", "apos": "'", "#39": "'",
}

// decodeEntities expands the handful of entities that appear in scraped text.
func decodeEntities(s string) string {
	if !strings.ContainsRune(s, '&') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != '&' {
			b.WriteByte(s[i])
			i++
			continue
		}
		end := strings.IndexByte(s[i:], ';')
		if end < 0 || end > 10 {
			b.WriteByte('&')
			i++
			continue
		}
		body := s[i+1 : i+end]
		if replacement, ok := namedEntities[strings.ToLower(body)]; ok {
			b.WriteString(replacement)
			i += end + 1
			continue
		}
		if r, ok := numericEntity(body); ok {
			b.WriteRune(r)
			i += end + 1
			continue
		}
		b.WriteByte('&')
		i++
	}
	return b.String()
}

func numericEntity(body string) (rune, bool) {
	if len(body) < 2 || body[0] != '#' {
		return 0, false
	}
	base, digits := 10, body[1:]
	if digits[0] == 'x' || digits[0] == 'X' {
		base, digits = 16, digits[1:]
	}
	code, err := strconv.ParseInt(digits, base, 32)
	if err != nil || code <= 0 || code > 0x10FFFF {
		return 0, false
	}
	return rune(code), true
}
