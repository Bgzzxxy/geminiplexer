package gemini

import (
	"regexp"
)

var (
	citePattern       = regexp.MustCompile(`[ \t]*\[cite:\s*[0-9,\s]+\]`)
	citeStartPattern  = regexp.MustCompile(`[ \t]*\[cite_start\]`)
	codeFenceSuffixes = regexp.MustCompile(`\?(?:code_reference|code_stdout)`)
	spaceBeforePunct  = regexp.MustCompile(`[ \t]+([.,;:!?])`)
)

func SanitizeText(input string) string {
	res := citePattern.ReplaceAllString(input, "")
	res = citeStartPattern.ReplaceAllString(res, "")
	res = codeFenceSuffixes.ReplaceAllString(res, "")
	res = spaceBeforePunct.ReplaceAllString(res, "$1")
	return res
}
