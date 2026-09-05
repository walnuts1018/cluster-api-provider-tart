package network

import "strconv"

func unmarshalTextJSON(value []byte, unmarshal func([]byte) error) error {
	text, err := strconv.Unquote(string(value))
	if err != nil {
		return err
	}
	return unmarshal([]byte(text))
}
