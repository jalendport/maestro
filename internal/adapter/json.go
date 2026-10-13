package adapter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
)

// Objects use a slice so decoding and encoding preserve each key's position.
type member struct {
	key   string
	value *value
}
type value struct {
	kind   byte
	object []member
	array  []*value
	scalar any
}

func object(fields ...member) *value { return &value{kind: '{', object: fields} }
func array(items ...*value) *value   { return &value{kind: '[', array: items} }
func scalar(v any) *value            { return &value{scalar: v} }

func (v *value) get(key string) *value {
	if v == nil || v.kind != '{' {
		return nil
	}
	for _, m := range v.object {
		if m.key == key {
			return m.value
		}
	}
	return nil
}
func (v *value) set(key string, item *value) {
	for i := range v.object {
		if v.object[i].key == key {
			v.object[i].value = item
			return
		}
	}
	v.object = append(v.object, member{key, item})
}

func decode(data []byte) (*value, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	v, err := readValue(d)
	if err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("unexpected content after JSON value")
		}
		return nil, err
	}
	return v, nil
}

func readValue(d *json.Decoder) (*value, error) {
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return scalar(token), nil
	}
	v := &value{kind: byte(delim)}
	switch delim {
	case '{':
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return nil, err
			}
			item, err := readValue(d)
			if err != nil {
				return nil, err
			}
			v.object = append(v.object, member{key.(string), item})
		}
	case '[':
		for d.More() {
			item, err := readValue(d)
			if err != nil {
				return nil, err
			}
			v.array = append(v.array, item)
		}
	default:
		return nil, fmt.Errorf("unexpected delimiter %q", delim)
	}
	if _, err := d.Token(); err != nil {
		return nil, err
	}
	return v, nil
}

func (v *value) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	switch v.kind {
	case '{':
		b.WriteByte('{')
		for i, m := range v.object {
			if i > 0 {
				b.WriteByte(',')
			}
			key, err := marshalScalar(m.key)
			if err != nil {
				return nil, err
			}
			b.Write(key)
			b.WriteByte(':')
			item, err := m.value.MarshalJSON()
			if err != nil {
				return nil, err
			}
			b.Write(item)
		}
		b.WriteByte('}')
	case '[':
		b.WriteByte('[')
		for i, v := range v.array {
			if i > 0 {
				b.WriteByte(',')
			}
			item, err := v.MarshalJSON()
			if err != nil {
				return nil, err
			}
			b.Write(item)
		}
		b.WriteByte(']')
	default:
		return marshalScalar(v.scalar)
	}
	return b.Bytes(), nil
}

// marshalScalar keeps &, <, and > as written in human-authored commands.
func marshalScalar(v any) ([]byte, error) {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err := e.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}

// Equality ignores object key order but retains array order and value types.
func equal(a, b *value) bool {
	if a == nil || b == nil {
		return a == b
	}
	ab, err := a.MarshalJSON()
	if err != nil {
		return false
	}
	bb, err := b.MarshalJSON()
	if err != nil {
		return false
	}
	var av, bv any
	if json.Unmarshal(ab, &av) != nil || json.Unmarshal(bb, &bv) != nil {
		return false
	}
	return reflect.DeepEqual(av, bv)
}

func indent(data []byte) string {
	for _, line := range strings.Split(string(data), "\n") {
		prefix := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		if prefix != "" && strings.TrimSpace(line) != "" {
			return prefix
		}
	}
	return "  "
}

func encode(v *value, spacing string) ([]byte, error) {
	compact, err := v.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := json.Indent(&b, compact, "", spacing); err != nil {
		return nil, err
	}
	b.WriteByte('\n')
	return b.Bytes(), nil
}
