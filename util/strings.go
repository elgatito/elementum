package util

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

func RandomString(length int) string {
	alphabet := []rune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz")
	var sb strings.Builder

	for range length {
		sb.WriteRune(alphabet[rand.IntN(len(alphabet))])
	}

	return sb.String()
}

// ApplyColor applies a color to a text to use in Kodi skin
func ApplyColor(text string, color string) (coloredText string) {
	coloredText = text
	if color != "" && color != "none" {
		coloredText = fmt.Sprintf(`[COLOR %s]%s[/COLOR]`, color, text)
	}
	return
}
