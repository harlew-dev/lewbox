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

// raw is the field's JSON in a request body, or nil when it was absent.
func (o OptionalInt) raw() json.RawMessage {
	if !o.Set {
		return nil
	}
	if o.Value == nil {
		return json.RawMessage("null")
	}
	data, _ := json.Marshal(*o.Value)
	return data
}
