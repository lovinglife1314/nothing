package naming

import (
	"fmt"

	"google.golang.org/grpc"
)

var (
	errMetaNotMap = fmt.Errorf("metadata is not map")
)

type Selector interface {
	Select(grpc.Address) (bool, error)
}

type MetaSelector struct {
	FieldName string
	Target    string
}

// And returns Selector that return true if all ss return true
func And(ss ...Selector) Selector {
	return andSelector{selectors: ss}
}

// Or returns Selector that return true if any ss return true, if there is any error
// returns the first error encounters
func Or(ss ...Selector) Selector {
	return orSelector{selectors: ss}
}

// True returns Selector that always return true
func True() Selector {
	return constSelector{true, nil}
}

// False returns Selector that always return false
func False() Selector {
	return constSelector{false, nil}
}

// Error returns Selector that always return error
func Error(err error) Selector {
	return constSelector{false, err}
}

func (ms MetaSelector) Select(addr grpc.Address) (bool, error) {
	metam, ok := addr.Metadata.(map[string]interface{})
	if !ok {
		return false, errMetaNotMap
	}

	fieldv, ok := metam[ms.FieldName]
	if !ok {
		return false, nil
	}

	fields, ok := fieldv.(string)
	if !ok {
		return false, nil
	}

	return fields == ms.Target, nil
}

type andSelector struct {
	selectors []Selector
}

func (as andSelector) Select(addr grpc.Address) (bool, error) {
	for _, s := range as.selectors {
		ok, err := s.Select(addr)
		if err != nil {
			return ok, err
		}

		if !ok {
			return false, nil
		}
	}

	return true, nil
}

type orSelector struct {
	selectors []Selector
}

func (os orSelector) Select(addr grpc.Address) (bool, error) {
	for _, s := range os.selectors {
		ok, err := s.Select(addr)
		if err != nil {
			return ok, err
		}

		if ok {
			return true, nil
		}
	}

	return false, nil
}

type constSelector struct {
	ok  bool
	err error
}

func (cs constSelector) Select(addr grpc.Address) (bool, error) {
	return cs.ok, cs.err
}
