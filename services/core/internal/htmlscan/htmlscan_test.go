package htmlscan_test

import (
	"strings"
	"testing"

	"github.com/minhhdtr/xsmb-discord-bot/internal/htmlscan"
)

func check(t *testing.T, html, class string, want ...string) {
	t.Helper()
	got := htmlscan.TextByClass(html, class)
	if len(got) != len(want) {
		t.Fatalf("got %d matches %q, want %d %q", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("match %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFlatCells(t *testing.T) {
	check(t, `<table><tr><td class="prize7">20</td><td class="prize7">89</td></tr></table>`,
		"prize7", "20", "89")
}

func TestNestedTextIsConcatenated(t *testing.T) {
	check(t, `<div class="special-prize"><span>94</span><span>533</span></div>`,
		"special-prize", "94533")
}

func TestNestedMatchIsNotReportedTwice(t *testing.T) {
	check(t, `<div class="prize1"><span class="prize1">52552</span></div>`,
		"prize1", "52552")
}

func TestSiblingsAfterNestedMatchStillCaptured(t *testing.T) {
	check(t, `<div class="p"><b class="p">1</b></div><div class="p">2</div>`,
		"p", "1", "2")
}

func TestUnclosedTableCells(t *testing.T) {
	check(t, `<table><tr><td class="prize6">361<td class="prize6">456<td class="prize6">371</table>`,
		"prize6", "361", "456", "371")
}

func TestClassTokenMustMatchWhole(t *testing.T) {
	html := `<td class="prize1">A</td><td class="prize10">B</td><td class="xprize1">C</td>`
	check(t, html, "prize1", "A")
}

func TestMultipleClassesOnOneElement(t *testing.T) {
	check(t, `<td class="  cell prize2  highlight ">58046</td>`, "prize2", "58046")
}

func TestUnquotedAndSingleQuotedAttributes(t *testing.T) {
	check(t, `<td class=prize3>02502</td><td class='prize3'>30375</td>`,
		"prize3", "02502", "30375")
}

func TestSimilarlyNamedAttributeIgnored(t *testing.T) {
	check(t, `<td data-class="prize4" class="other">X</td><td class="prize4">7941</td>`,
		"prize4", "7941")
}

func TestUppercaseTags(t *testing.T) {
	check(t, `<TABLE><TD CLASS="prize5">7738</TD></TABLE>`, "prize5", "7738")
}

func TestCommentsSkipped(t *testing.T) {
	check(t, `<!-- <td class="prize7">99</td> --><td class="prize7">20</td>`, "prize7", "20")
}

func TestDoctypeAndProcessingInstructionSkipped(t *testing.T) {
	check(t, `<!DOCTYPE html><?xml version="1.0"?><td class="prize7">20</td>`, "prize7", "20")
}

func TestScriptBodyNotRead(t *testing.T) {
	html := `<script>var x = '<td class="prize7">99</td>';</script><td class="prize7">20</td>`
	check(t, html, "prize7", "20")
}

func TestStyleBodyNotRead(t *testing.T) {
	check(t, `<style>.prize7 { content: "<td>"; }</style><td class="prize7">20</td>`, "prize7", "20")
}

func TestGreaterThanInsideAttributeValue(t *testing.T) {
	check(t, `<td title="a > b" class="prize7">20</td>`, "prize7", "20")
}

func TestBrSeparatesNumbers(t *testing.T) {
	check(t, `<div class="prize3">02502<br>30375<br/>80553</div>`,
		"prize3", "02502 30375 80553")
}

func TestBlockChildrenSeparate(t *testing.T) {
	check(t, `<div class="prize2"><div>58046</div><div>94227</div></div>`,
		"prize2", "58046 94227")
}

func TestInlineSpansAreOneNumber(t *testing.T) {
	check(t, `<div class="special-prize"><span>94</span><span>533</span></div>`,
		"special-prize", "94533")
}

func TestInlineVoidElementDoesNotSeparate(t *testing.T) {
	check(t, `<div class="k">9<img src="x.png">4</div>`, "k", "94")
}

func TestSelfClosingInlineTag(t *testing.T) {
	check(t, `<div class="k">1<span/>2</div>`, "k", "12")
}

func TestSiblingBlocksStillYieldSeparateMatches(t *testing.T) {
	check(t, `<div class="k">9</div><div class="k">7</div>`, "k", "9", "7")
}

func TestWhitespaceCollapsed(t *testing.T) {
	check(t, "<td class=\"k\">\n\t  94533 \r\n </td>", "k", "94533")
}

func TestEntitiesDecoded(t *testing.T) {
	check(t, `<td class="k">9&nbsp;4&amp;5&#48;&#x31;</td>`, "k", "9\u00a04&501")
}

func TestUnknownEntityLeftAlone(t *testing.T) {
	check(t, `<td class="k">a&bogus;b</td>`, "k", "a&bogus;b")
}

func TestTruncatedDocumentStillEmits(t *testing.T) {
	check(t, `<td class="k">94533`, "k", "94533")
}

func TestStrayEndTagIgnored(t *testing.T) {
	check(t, `</div><td class="k">20</td></span>`, "k", "20")
}

func TestOuterEndTagClosesCapture(t *testing.T) {
	check(t, `<table><td class="k">20</table><td class="k">89</td>`, "k", "20", "89")
}

func TestNoMatchReturnsEmpty(t *testing.T) {
	if got := htmlscan.TextByClass(`<td class="a">1</td>`, "b"); len(got) != 0 {
		t.Fatalf("got %q, want none", got)
	}
}

func TestEmptyInput(t *testing.T) {
	if got := htmlscan.TextByClass("", "k"); len(got) != 0 {
		t.Fatalf("got %q", got)
	}
}

func TestBareAngleBracketInText(t *testing.T) {
	check(t, `<td class="k">3 < 5</td>`, "k", "3 < 5")
}

func TestDeeplyNestedDoesNotStackOverflow(t *testing.T) {
	html := strings.Repeat(`<div>`, 5000) + `<td class="k">20</td>` + strings.Repeat(`</div>`, 5000)
	check(t, html, "k", "20")
}

func TestDuplicatedTableYieldsBothCopies(t *testing.T) {
	cell := `<td class="k">20</td>`
	check(t, `<table>`+cell+`</table><table>`+cell+`</table>`, "k", "20", "20")
}
