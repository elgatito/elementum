package util

import (
	"fmt"
	"math/rand"
	"strings"
)

func RandomString(length int) string {
	var alphabet []rune = []rune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz")

	alphabetSize := len(alphabet)
	var sb strings.Builder

	for i := 0; i < length; i++ {
		ch := alphabet[rand.Intn(alphabetSize)]
		sb.WriteRune(ch)
	}

	s := sb.String()
	return s
}

// ApplyColor applies a color to a text to use in Kodi skin
func ApplyColor(text string, color string) (coloredText string) {
	coloredText = text
	if color != "" && color != "none" {
		coloredText = fmt.Sprintf(`[COLOR %s]%s[/COLOR]`, color, text)
	}
	return
}
