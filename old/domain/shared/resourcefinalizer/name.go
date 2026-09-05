package resourcefinalizer

import "fmt"

type Name struct {
	value string
}

func ParseName(value string) (Name, error) {
	if value == "" {
		return Name{}, fmt.Errorf("resource finalizer name must not be empty")
	}
	return Name{value: value}, nil
}

func MustName(value string) Name {
	name, err := ParseName(value)
	if err != nil {
		panic(err)
	}
	return name
}

func (name Name) String() string {
	return name.value
}
