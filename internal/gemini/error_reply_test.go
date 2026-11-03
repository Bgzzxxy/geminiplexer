package gemini

import "testing"

func TestGeminiErrorReplyDetectsCannedApologies(t *testing.T) {
	cases := []string{
		"Sorry, something went wrong. Please try your request again.",
		"I seem to be encountering an error. Can I try something else for you?",
		"I encountered an error doing what you asked. Could you try again?",
	}
	for _, in := range cases {
		if got := GeminiErrorReply(in); got == "" {
			t.Errorf("expected canned error for %q, got none", in)
		}
	}
}

// A genuine answer that merely quotes an apology must still pass: it is longer
// than geminiErrorReplyMaxChars and does not match an anywhere-phrase.
func TestGeminiErrorReplyAllowsGenuineAnswers(t *testing.T) {
	genuine := []string{
		"The upstream returned \"Sorry, something went wrong. Please try your request again.\" earlier, so I retried the request and it succeeded.",
		"BANANA-42",
		"1\n2\n3\n4\n5",
	}
	for _, in := range genuine {
		if got := GeminiErrorReply(in); got != "" {
			t.Errorf("genuine answer %q wrongly flagged as %q", in, got)
		}
	}
}
