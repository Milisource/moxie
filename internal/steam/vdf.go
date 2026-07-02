package steam

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// vdfBuffer represents a method of storing binary data.
type vdfBuffer struct {
	Data     []byte
	Position uint32
}

// vdfMap represents a VDF file map (map[string]any).
type vdfMap map[string]any

const (
	vdfMapStart byte = 0x00
	vdfString        = 0x01
	vdfNumber        = 0x02
	vdfMapEnd        = 0x08
)

type vdfMapItem struct {
	Type  byte
	Name  string
	Value any
}

// readVdf reads a binary VDF byte slice into a vdfMap.
func readVdf(data []byte) (vdfMap, error) {
	buf := &vdfBuffer{Data: data, Position: 0}
	return nextMap(buf)
}

// nextMap reads the next map from the buffer.
func nextMap(buf *vdfBuffer) (vdfMap, error) {
	contents := make(vdfMap)
	for {
		item, err := nextMapItem(buf)
		if err != nil {
			return nil, err
		}
		if item.Type == vdfMapEnd {
			break
		}
		contents[item.Name] = item.Value
	}
	return contents, nil
}

// nextMapItem reads the next item from the buffer.
func nextMapItem(buf *vdfBuffer) (vdfMapItem, error) {
	if int(buf.Position) >= len(buf.Data) {
		return vdfMapItem{}, errors.New("vdf: unexpected end of data")
	}

	typeByte := buf.Data[buf.Position]
	buf.Position++

	if typeByte == vdfMapEnd {
		return vdfMapItem{Type: vdfMapEnd}, nil
	}

	name, err := nextStringZero(buf)
	if err != nil {
		return vdfMapItem{}, err
	}

	var value any
	switch typeByte {
	case vdfMapStart:
		value, err = nextMap(buf)
		if err != nil {
			return vdfMapItem{}, err
		}
	case vdfString:
		value, err = nextStringZero(buf)
		if err != nil {
			return vdfMapItem{}, err
		}
	case vdfNumber:
		if int(buf.Position)+4 > len(buf.Data) {
			return vdfMapItem{}, errors.New("vdf: unexpected end of data reading number")
		}
		value = binary.LittleEndian.Uint32(buf.Data[buf.Position : buf.Position+4])
		buf.Position += 4
	default:
		return vdfMapItem{}, fmt.Errorf("vdf: unrecognized type byte 0x%02x", typeByte)
	}

	return vdfMapItem{Type: typeByte, Name: name, Value: value}, nil
}

// nextStringZero reads a null-terminated string from the buffer.
func nextStringZero(buf *vdfBuffer) (string, error) {
	if buf.Position >= uint32(len(buf.Data)) {
		return "", errors.New("vdf: aborted buffer overflow")
	}
	start := buf.Position
	end := buf.Position
	for {
		if int(buf.Position) >= len(buf.Data) {
			return "", errors.New("vdf: unterminated string")
		}
		c := buf.Data[buf.Position]
		buf.Position++
		if c == 0 {
			break
		}
		end++
	}
	return string(buf.Data[start:end]), nil
}

// writeVdf serializes a vdfMap to binary VDF bytes.
func writeVdf(m vdfMap) ([]byte, error) {
	return addMap(m)
}

// addMap serializes a vdfMap.
func addMap(m vdfMap) ([]byte, error) {
	var buf []byte
	for k, v := range m {
		switch val := v.(type) {
		case uint32:
			kt, err := addKT(vdfNumber, k)
			if err != nil {
				return nil, err
			}
			buf = append(buf, kt...)
			bytes := make([]byte, 4)
			binary.LittleEndian.PutUint32(bytes, val)
			buf = append(buf, bytes...)
		case string:
			kt, err := addKT(vdfString, k)
			if err != nil {
				return nil, err
			}
			buf = append(buf, kt...)
			bytes, err := addString(val)
			if err != nil {
				return nil, err
			}
			buf = append(buf, bytes...)
		case vdfMap:
			kt, err := addKT(vdfMapStart, k)
			if err != nil {
				return nil, err
			}
			buf = append(buf, kt...)
			bytes, err := addMap(val)
			if err != nil {
				return nil, err
			}
			buf = append(buf, bytes...)
		default:
			return nil, fmt.Errorf("vdf: unrecognized Go type %T for key %q", v, k)
		}
	}
	buf = append(buf, vdfMapEnd)
	return buf, nil
}

func addString(value string) ([]byte, error) {
	bytes := []byte(value)
	for _, b := range bytes {
		if b == 0 {
			return nil, errors.New("vdf: NUL terminator found in string value")
		}
	}
	bytes = append(bytes, 0)
	return bytes, nil
}

func addKT(typeByte byte, key string) ([]byte, error) {
	keyBytes, err := addString(key)
	if err != nil {
		return nil, err
	}
	return append([]byte{typeByte}, keyBytes...), nil
}

// validateShortcutsVDF checks that a parsed vdfMap has the expected structure
// for a shortcuts.vdf file: it must contain a "shortcuts" key whose value is a map.
func validateShortcutsVDF(m vdfMap) error {
	if m == nil {
		return errors.New("vdf: parsed map is nil")
	}
	raw, ok := m["shortcuts"]
	if !ok {
		return errors.New("vdf: missing required 'shortcuts' key")
	}
	if _, ok := raw.(vdfMap); !ok {
		return fmt.Errorf("vdf: 'shortcuts' key is type %T, expected map", raw)
	}
	return nil
}
