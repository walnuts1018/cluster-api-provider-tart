// Package textmarshalは、文字列ベースのdomain値オブジェクトが持つMarshalJSON/UnmarshalJSONの
// 定型実装を提供する。全ての値オブジェクトはencoding.TextMarshaler/TextUnmarshalerを実装済みであるため、
// このpackageはJSON文字列リテラルとの相互変換だけを担い、値の意味解釈には関与しない。
package textmarshal

import "strconv"

// JSONはstringのJSON表現を返す。値オブジェクトのMarshalJSONはこれをStringへ委譲するだけでよい。
func JSON(s string) ([]byte, error) {
	return strconv.AppendQuote(nil, s), nil
}

// UnmarshalJSONはJSON文字列リテラルvalueをデコードし、その結果をunmarshalTextへ渡す。
// 値オブジェクトのUnmarshalJSONは、既存のUnmarshalText実装をこれへ渡すだけでよい。
// JSON null(strconv.Unquoteでは扱えない非文字列literal)は空値としてunmarshalTextへ渡し、
// 各値オブジェクトのゼロ値への復元に委ねる。
func UnmarshalJSON(value []byte, unmarshalText func([]byte) error) error {
	if string(value) == "null" {
		return unmarshalText(nil)
	}
	text, err := strconv.Unquote(string(value))
	if err != nil {
		return err
	}
	return unmarshalText([]byte(text))
}
