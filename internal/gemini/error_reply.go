package gemini

import (
	"regexp"
	"strings"
)

// Gemini Web answers a blocked or failed request with HTTP 200 and a canned
// apology as the answer text. Without this detector those apologies are served
// to clients as if they were real completions.
//
// Ported from gemini-stealth-bridge: phrases that are unambiguous errors are
// matched anywhere in a short reply, while full-sentence apologies must match
// exactly (or as a prefix) and stay under geminiErrorReplyMaxChars, so a
// genuine answer that merely quotes one of them still passes through.
var (
	geminiErrorReplies = []string{
		"sorry, something went wrong. please try your request again.",
		"sorry, something went wrong. please try again.",
		"something went wrong. please try your request again.",
		"an error occurred. please try again.",
		"i encountered an error doing what you asked.",
		"i encountered an error while doing what you asked.",
		"i ran into an error while doing what you asked.",
		"i'm a language model, and i'm not designed to do that.",
		"i am a language model, and i am not designed to do that.",
		"i'm just a language model, so i can't help with that.",
		"i am just a language model, so i cannot help with that.",
		"i'm not programmed to do that.",
		"i am not programmed to do that.",
		"i'm not able to help with that, as i'm only a language model.",
		"i am not able to help with that, as i am only a language model.",
		"i'm still learning how to answer this question.",
		"saya adalah model bahasa, dan saya tidak dirancang untuk itu.",
		"saya hanya model bahasa, jadi saya tidak bisa membantu terkait hal itu.",
		"saya hanya model bahasa dan tidak bisa membantu anda untuk itu.",
		"saya hanya model bahasa dan tidak dapat membantu anda untuk itu.",
		"saya tidak bisa membantu terkait hal itu karena saya hanya model bahasa.",
	}

	geminiErrorReplyAnywhere = []string{
		"i encountered an error doing what you asked",
		"i encountered an error while doing what you asked",
		"i ran into an error while doing what you asked",
		"i'm having a hard time fulfilling your request",
		"i am having a hard time fulfilling your request",
		"i seem to be encountering an error",
		"i seem to be having trouble",
		"saya adalah model bahasa, dan saya tidak dirancang untuk itu",
		"saya hanya model bahasa, jadi saya tidak bisa membantu",
		"saya hanya model bahasa dan tidak bisa membantu",
		"saya hanya model bahasa dan tidak dapat membantu",
		"saya tidak diprogram untuk melakukan hal itu",
		"i'm not programmed to do that",
	}

	geminiCannedRefusalRe = regexp.MustCompile(`(?i)^(i'm sorry|i am sorry|sorry)[,.]? (but )?i (can't|cannot|am unable to|'m unable to) (help|assist) with that`)
)

const (
	geminiErrorReplyMaxChars         = 200
	geminiErrorReplyAnywhereMaxChars = 400
)

// GeminiErrorReply returns the matched canned-error phrase, or "" when the text
// looks like a genuine answer.
func GeminiErrorReply(text string) string {
	t := strings.TrimSpace(text)
	if t == "" {
		return ""
	}
	lower := strings.ToLower(t)
	if len(t) <= geminiErrorReplyAnywhereMaxChars {
		for _, s := range geminiErrorReplyAnywhere {
			if strings.Contains(lower, s) {
				return s
			}
		}
	}
	if len(t) > geminiErrorReplyMaxChars {
		return ""
	}
	for _, s := range geminiErrorReplies {
		bare := strings.TrimSuffix(s, ".")
		if lower == s || lower == bare || strings.HasPrefix(lower, bare) {
			return s
		}
	}
	if geminiCannedRefusalRe.MatchString(t) {
		return lower
	}
	return ""
}
