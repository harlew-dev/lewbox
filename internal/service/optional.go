package service

import "encoding/json"

// OptionalInt distinguishes the three states a PATCH body can give a number:
// absent (no change), null (clear it) and a value (set it). The zero value is
// absent.
type OptionalInt struct {
	Set   bool
	Value *int64
}

// UnmarshalJSON records even a null, which a plain pointer could not.
func (o *OptionalInt) UnmarshalJSON(data []byte) error {
	o.Set = true
	if string(data) == "null" {
		o.Value = nil
		return nil
	}
	var n int64
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	o.Value = &n
	return nil
}

// MarshalJSON keeps null when the caller cleared the field.
func (o OptionalInt) MarshalJSON() ([]byte, error) {
	if o.Value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(*o.Value)
}

// IsZero reports the absent state, so omitzero drops an unset field.
func (o OptionalInt) IsZero() bool { return !o.Set }
