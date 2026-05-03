package random

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	mathrand "math/rand/v2"
	"strings"
)

const UpperLetters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
const LowerLetters = "abcdefghijklmnopqrstuvwxyz"
const Numbers = "0123456789"
const Symbols = "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~"
const Alphabets = UpperLetters + LowerLetters
const Alphanumeric = Alphabets + Numbers
const AlphanumericSymbols = Alphanumeric + Symbols

type Random interface {
	InsecureString(length uint, base string) string
	SecureString(length uint, base string) (string, error)
}

type random struct{}

func New() Random {
	return random{}
}

func (r random) InsecureString(length uint, base string) string {
	runes := []rune(base)
	result := make([]rune, length)
	for i := range result {
		result[i] = runes[mathrand.IntN(len(runes))]
	}
	return string(result)
}

func (r random) SecureString(length uint, base string) (string, error) {
	if len(base) == 0 {
		return "", errors.New("base must not be empty")
	}

	// crypto/rand.Int で均一な分布のインデックスを生成し、modulo バイアスを排除します。
	baseLen := big.NewInt(int64(len(base)))
	var sb strings.Builder
	sb.Grow(int(length))
	for range length {
		idx, err := rand.Int(rand.Reader, baseLen)
		if err != nil {
			return "", fmt.Errorf("failed to generate random index: %w", err)
		}
		sb.WriteByte(base[idx.Int64()])
	}
	return sb.String(), nil
}

type dummy struct{}

func NewDummy() Random {
	return dummy{}
}

func (d dummy) InsecureString(length uint, base string) string {
	return "dummy"
}

func (d dummy) SecureString(length uint, base string) (string, error) {
	return "dummy", nil
}
