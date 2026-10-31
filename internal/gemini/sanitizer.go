package gemini

import (
	"regexp"
	"strings"
)

var (
	citePattern       = regexp.MustCompile(`\[cite:\s*[0-9,\s]+\]`)
	citeStartPattern  = regexp.MustCompile(`\[cite_start\]`)
	codeFenceSuffixes = regexp.MustCompile(`\?(?:code_reference|code_stdout)`)
	spaceBeforePunct  = regexp.MustCompile(`\s+([.,;:!?])`)
	multipleSpaces    = regexp.MustCompile(`[ 	]+`)
)

func SanitizeText(input string) string {
	res := citePattern.ReplaceAllString(input, "")
	res = citeStartPattern.ReplaceAllString(res, "")
	res = codeFenceSuffixes.ReplaceAllString(res, "")
	res = spaceBeforePunct.ReplaceAllString(res, "$1")
	res = multipleSpaces.ReplaceAllString(res, " ")
	return strings.TrimSpace(res)
}
