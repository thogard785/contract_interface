package thoggywoggy

import (
	"encoding/json"
	"errors"
	"math/big"
	"reflect"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

// meant to copy pasta'd inside of geth/bor in a convenient spot for your implementation
// this is not meant to be a complete package nor should it be used by anyone,
// it merely serves as an example of how others might implement their own version
// of a similar package.

var (
	ErrCantFindMethodABI      = errors.New("err - cant find method in ABI")
	ErrCantFindStructFieldABI = errors.New("err - cant find struct field in ABI")
	ErrStructFieldOutOfRange  = errors.New("err - struct field index out of range")
	ErrCantFindMethodJSON     = errors.New("err - cant find method in JSON tx args")
	ErrNotDynamicType         = errors.New("err - not dynamic type")
	ErrCantAssignField        = errors.New("err - cant assign source field to dest field")
	ErrCantSetDestField       = errors.New("err - cant set dest field")
)

type Contract struct {
	ABI     *abi.ABI
	Address common.Address
	Lock    sync.RWMutex
}

func NewContract(contractAddress common.Address, abiRaw []byte) (*Contract, error) {
	abi, err := abi.JSON(strings.NewReader(string(abiRaw)))
	if err != nil {
		log.Warn("err - loadABI", "error:", err)
		return nil, err
	}
	return &Contract{
		ABI:     &abi,
		Address: contractAddress,
	}, nil
}

func (c *Contract) getMethodNames() []string {
	c.Lock.RLock()
	defer c.Lock.RUnlock()
	methodNames := make([]string, len(c.ABI.Methods))
	n := 0
	for name, method := range c.ABI.Methods {
		if method.IsPayable() {
			methodNames[n] = name
			n++
		}
	}
	return methodNames
}

func (c *Contract) UpdateContractAddress(contractAddress common.Address) {
	c.Lock.Lock()
	defer c.Lock.Unlock()
	c.Address = contractAddress
}

func (c *Contract) getContractAddress() common.Address {
	c.Lock.RLock()
	defer c.Lock.RUnlock()
	contractAddress := c.Address
	return contractAddress
}

func (c *Contract) UpdateContractABI(abiRaw []byte) error {
	c.Lock.Lock()
	defer c.Lock.Unlock()
	abi, err2 := abi.JSON(strings.NewReader(string(abiRaw)))
	if err2 != nil {
		log.Warn("err - loadABI 2", "error:", err2)
		return err2
	}
	c.ABI = &abi
	return nil
}

// indirect recursively dereferences the value until it either gets the value
// or finds a big.Int
// copied from reflect package
func indirect(v reflect.Value) reflect.Value {
	if v.Kind() == reflect.Ptr && v.Elem().Type() != reflect.TypeOf(big.Int{}) {
		return indirect(v.Elem())
	}
	return v
}

func handleNestedUnmarshal(data json.RawMessage, t abi.Type) (interface{}, error) {
	switch t.T {
	case abi.TupleTy:

		newMap := make(map[string]json.RawMessage, len(t.TupleElems))
		if err := json.Unmarshal(data, &newMap); err != nil {
			return nil, err
		}

		newTuple := indirect(reflect.New(t.GetType()))
		for i, fieldName := range t.TupleRawNames {
			if _, ok := newMap[fieldName]; !ok {
				log.Warn("err - encodeTx 1.b.1", "index", i, "missing field name", fieldName)
				n := 0
				for _fieldName := range newMap {
					log.Warn("err - encodeTx 1.b.2", "index", n, "existing field name", _fieldName)
				}
				return nil, ErrCantFindStructFieldABI
			}

			newValue, err := handleNestedUnmarshal(newMap[fieldName], *t.TupleElems[i])
			if err != nil {
				return nil, err
			}

			if i >= newTuple.NumField() {
				log.Warn("err - encodeTx 2.b.1", "index", i, "missing field name", fieldName)
				n := 0
				for _fieldName := range newMap {
					log.Warn("err - encodeTx 2.b.2", "index", n, "existing field name", _fieldName)
				}
				return nil, ErrStructFieldOutOfRange
			}

			dst := newTuple.Field(i)
			src := indirect(reflect.ValueOf(newValue))

			if !dst.CanSet() {
				log.Warn("err - encodeTx 3.b.1", "index", i, "field name", fieldName, "err", ErrCantSetDestField)
				return nil, ErrCantSetDestField
			}

			if !src.Type().AssignableTo(dst.Type()) {
				log.Warn("err - encodeTx 3.b.2", "index", i, "field name", fieldName, "srcType", src.Type().Name(), "dstType", dst.Type().Name())
				return nil, ErrCantAssignField
			}

			dst.Set(src)
		}

		return newTuple.Interface(), nil

	case abi.SliceTy, abi.ArrayTy:
		var newSlice []json.RawMessage
		if err := json.Unmarshal(data, &newSlice); err != nil {
			log.Warn("err - encodeTx 4.b", "name", t.GetType().Name(), "err", err)
			return nil, err
		}

		newValueSlice := reflect.MakeSlice(t.GetType(), len(newSlice), len(newSlice)) //.Interface()

		for i, value := range newSlice {
			newValue, err := handleNestedUnmarshal(value, *t.Elem)
			if err != nil {
				log.Warn("err - encodeTx 5.b", "index", i, "name", t.GetType().Name(), "err", err)
				return nil, err
			}

			dst := newValueSlice.Index(i)
			src := indirect(reflect.ValueOf(newValue))

			if !dst.CanSet() {
				log.Warn("err - encodeTx 6.b.1", "index", i, "err", ErrCantSetDestField)
				return nil, ErrCantSetDestField
			}

			if !src.Type().AssignableTo(dst.Type()) {
				log.Warn("err - encodeTx 6.b.2", "index", i, "srcType", src.Type().Name(), "dstType", dst.Type().Name())
				return nil, ErrCantAssignField
			}

			dst.Set(src)
		}
		return newValueSlice.Interface(), nil

	case abi.FixedBytesTy:
		switch t.Size {
		case 32:
			var target [32]byte
			if err := json.Unmarshal(data, &target); err != nil {
				target = [32]byte{0}
			}
			if reflect.TypeOf(target) == reflect.TypeOf("") {
				target = [32]byte{0}
			}
			return target, nil
		case 8:
			var target [8]byte
			if err := json.Unmarshal(data, &target); err != nil {
				target = [8]byte{0}
			}
			if reflect.TypeOf(target) == reflect.TypeOf("") {
				target = [8]byte{0}
			}
			return target, nil
		default:
			var target []byte
			if err := json.Unmarshal(data, &target); err != nil {
				target = []byte{0}
			}
			if reflect.TypeOf(target) == reflect.TypeOf("") {
				target = []byte{0}
			}
			return target, nil
		}

	case abi.BytesTy:
		var target []byte
		if err := json.Unmarshal(data, &target); err != nil {
			target = []byte{0}
		}
		if reflect.TypeOf(target) == reflect.TypeOf("") {
			target = []byte{0}
		}
		return target, nil

	default:
		value := reflect.New(t.GetType()).Interface()
		json.Unmarshal(data, &value)
		return value, nil
	}
}

func (c *Contract) encodeTxData(funcName string, _argValues string) ([]byte, error) {
	// _argValues must be a JSON-format string
	c.Lock.RLock()
	defer c.Lock.RUnlock()

	method, ok := c.ABI.Methods[funcName]
	if !ok {
		return []byte{0}, ErrCantFindMethodABI
	}

	var argValues []json.RawMessage
	if err := json.Unmarshal([]byte(_argValues), &argValues); err != nil {
		log.Warn("err", "encodeTx 0a", err)
		return []byte{0}, err
	}

	values := make([]interface{}, len(argValues))
	for i := 0; i < len(method.Inputs); i++ {
		_value, _type := argValues[i], method.Inputs[i].Type
		value, err := handleNestedUnmarshal(_value, _type)
		if err != nil {
			log.Warn("err - encodeTx 1a", "index", i, "name", method.Inputs[i].Name, "err", err)
			return []byte{0}, err
		}
		values[i] = value
	}
	return c.ABI.Pack(funcName, values...)
}
