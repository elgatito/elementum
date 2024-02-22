package util

import (
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
